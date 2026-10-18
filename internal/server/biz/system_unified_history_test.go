package biz

import (
 "context"
 "errors"
 "fmt"
 "os"
 "sync"
 "strings"
 "testing"
 "time"

 "github.com/stretchr/testify/require"
 "github.com/looplj/axonhub/internal/authz"
 "github.com/looplj/axonhub/internal/contexts"
 "github.com/looplj/axonhub/internal/ent"
 "github.com/looplj/axonhub/internal/ent/enttest"
 "github.com/looplj/axonhub/internal/ent/system"
)

func TestUnifiedHistoryStorageTransactionAndRetention(t *testing.T) {
 client:=enttest.NewEntClient(t,"sqlite3","file:history-storage?mode=memory&_fk=1")
 t.Cleanup(func(){require.NoError(t,client.Close())})
 testUnifiedHistoryStorage(t,client)
}

func TestUnifiedHistoryStorage_Postgres(t *testing.T) {
 dsn:=os.Getenv("AXONHUB_TEST_PG_DSN")
 if dsn=="" {t.Skip("AXONHUB_TEST_PG_DSN not set")}
 testUnifiedHistoryStorage(t,newPostgresQuotaRoutingMigrationClient(t,dsn))
}

func testUnifiedHistoryStorage(t *testing.T,client *ent.Client) {
 t.Helper()
 service:=NewSystemService(SystemServiceParams{Ent:client,UnifiedHistory:UnifiedHistoryConfig{Enabled:true,RetentionDays:2}})
 ctx:=contexts.WithAPIKey(authz.WithTestBypass(ent.NewContext(t.Context(),client)),&ent.APIKey{ID:71,ProjectID:19})
 ctx=contexts.WithProjectID(ctx,19)
 entries:=map[string]string{}
 for i:=range 70 {entries[fmt.Sprintf("block:%064x",i)]="ciphertext"}
 rollback:=errors.New("publication rejected")
 err:=service.RunInTransaction(ctx,func(txCtx context.Context)error{if err:=service.SaveUnifiedHistory(txCtx,entries);err!=nil{return err};return rollback})
 require.ErrorIs(t,err,rollback)
 count,err:=client.System.Query().Count(ctx);require.NoError(t,err);require.Zero(t,count,"all SQL batches roll back together")
 require.NoError(t,service.SaveUnifiedHistory(ctx,entries))
 key:="block:"+strings.Repeat("0",64)
 require.NoError(t,service.SaveUnifiedHistory(ctx,map[string]string{key:"conflicting ciphertext"}))
 loaded,err:=service.LoadUnifiedHistory(ctx,[]string{key});require.NoError(t,err);require.Equal(t,"ciphertext",loaded[key])
 foreign:=contexts.WithAPIKey(contexts.CloneRequestContext(ctx),&ent.APIKey{ID:72,ProjectID:19})
 loaded,err=service.LoadUnifiedHistory(foreign,[]string{key});require.NoError(t,err);require.Empty(t,loaded)
 _,err=service.LoadUnifiedHistory(ctx,[]string{"block:../escape"});require.Error(t,err)
 _,err=service.LoadUnifiedHistory(t.Context(),[]string{key});require.Error(t,err)
 activeTime:=time.Now().Add(-36*time.Hour)
 require.NoError(t,client.System.Update().Where(system.KeyHasSuffix(key)).SetUpdatedAt(activeTime).Exec(ctx))
 _,err=service.LoadUnifiedHistory(ctx,[]string{key});require.NoError(t,err)
 active,err:=client.System.Query().Where(system.KeyHasSuffix(key)).Only(ctx);require.NoError(t,err);require.True(t,active.UpdatedAt.After(activeTime.Add(time.Hour)),"reading active history refreshes retention")
 old:=time.Now().Add(-4*24*time.Hour)
 require.NoError(t,client.System.Update().Where(system.KeyHasPrefix(unifiedHistoryPrefix)).SetUpdatedAt(old).Exec(ctx))
 loaded,err=service.LoadUnifiedHistory(ctx,[]string{key});require.NoError(t,err);require.Empty(t,loaded,"expired records are unreadable before GC")
 require.NoError(t,service.SaveUnifiedHistory(ctx,map[string]string{key:"new ciphertext"}))
 loaded,err=service.LoadUnifiedHistory(ctx,[]string{key});require.NoError(t,err);require.Equal(t,"ciphertext",loaded[key],"refresh preserves immutable ciphertext")
 require.NoError(t,client.System.Create().SetKey("unrelated_setting").SetValue("retained").SetUpdatedAt(old).Exec(ctx))
 require.NoError(t,service.PruneUnifiedHistory(ctx))
 count,err=client.System.Query().Count(ctx);require.NoError(t,err);require.Equal(t,2,count)
 service.UnifiedHistory.RetentionDays=0
 require.NoError(t,client.System.Update().SetUpdatedAt(old).Exec(ctx))
 require.NoError(t,service.PruneUnifiedHistory(ctx))
 count,err=client.System.Query().Count(ctx);require.NoError(t,err);require.Equal(t,2,count)
}

func TestUnifiedHistoryPublicationRace_Postgres(t *testing.T) {
 dsn:=os.Getenv("AXONHUB_TEST_PG_DSN")
 if dsn=="" {t.Skip("AXONHUB_TEST_PG_DSN not set")}
 client:=newPostgresQuotaRoutingMigrationClient(t,dsn)
 service:=NewSystemService(SystemServiceParams{Ent:client,UnifiedHistory:UnifiedHistoryConfig{Enabled:true}})
 ctx:=contexts.WithProjectID(contexts.WithAPIKey(authz.WithTestBypass(ent.NewContext(t.Context(),client)),&ent.APIKey{ID:71,ProjectID:19}),19)
 alias:="response:"+strings.Repeat("a",64)
 conflict:=errors.New("immutable alias conflict")
 results:=make(chan error,8)
 start:=make(chan struct{})
 var wg sync.WaitGroup
 for i:=range 8 {
  wg.Go(func(){
   <-start
   value:=fmt.Sprintf("writer-%d",i)
   results<-service.RunInTransaction(ctx,func(txCtx context.Context)error {
    if err:=service.SaveUnifiedHistory(txCtx,map[string]string{alias:value,fmt.Sprintf("block:%064x",i):value});err!=nil{return err}
    stored,err:=service.LoadUnifiedHistory(txCtx,[]string{alias});if err!=nil{return err}
    if stored[alias]!=value{return conflict};return nil
   })
  })
 }
 close(start);wg.Wait();close(results)
 winners:=0
 for err:=range results {if err==nil{winners++}else{require.ErrorIs(t,err,conflict)}}
 require.Equal(t,1,winners)
 count,err:=client.System.Query().Where(system.KeyHasPrefix(unifiedHistoryPrefix)).Count(ctx)
 require.NoError(t,err);require.Equal(t,2,count,"losing writers must not leave unreachable blocks")
}
