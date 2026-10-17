package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
)

func TestNativeCompactionMigrationStorageAndIsolation(t *testing.T) {
	ctx,client,owner,adapter:=rejectedCompactionStorage(t)
	key:=responsesCompactionRecoveryKey{apiKeyID:owner.ID,projectID:owner.ProjectID,cacheKey:"source-checkpoint-hash",threadID:"migration-thread"}
	key.scope.provider.channelID=41
	key.scope.provider.model="gpt-6-astra"
	key.scope.provider.url="https://example.invalid/v1/responses"
	key.scope.provider.credential[0]=7
	item:=json.RawMessage(`{"type":"compaction","id":"cmp_target","encrypted_content":"opaque-target"}`)
	require.NoError(t,adapter.saveNativeMigration(ctx,key,nativeCompactionMigration{Item:item}))
	restarted:=newRemoteCompactionAdapter(adapter.requestService,nil,biz.NewSystemService(biz.SystemServiceParams{Ent:client}))
	value,err:=restarted.loadNativeMigration(ctx,key)
	require.NoError(t,err)
	require.JSONEq(t,string(item),string(value.Item))
	sealed,err:=adapter.systemService.LoadNativeCompactionMigration(ctx,key.digest())
	require.NoError(t,err)
	for _,mutate:=range []func(*responsesCompactionRecoveryKey){
		func(k *responsesCompactionRecoveryKey){k.apiKeyID++},
		func(k *responsesCompactionRecoveryKey){k.projectID++},
		func(k *responsesCompactionRecoveryKey){k.cacheKey="different ciphertext"},
		func(k *responsesCompactionRecoveryKey){k.threadID="different thread"},
		func(k *responsesCompactionRecoveryKey){k.scope.provider.channelID++},
		func(k *responsesCompactionRecoveryKey){k.scope.provider.credential[0]++},
		func(k *responsesCompactionRecoveryKey){k.scope.provider.model="another model"},
		func(k *responsesCompactionRecoveryKey){k.scope.provider.url+="/other"},
		func(k *responsesCompactionRecoveryKey){k.scope.windowID="other window"},
	} {
		other:=key
		mutate(&other)
		value,err:=restarted.loadNativeMigration(ctx,other)
		require.NoError(t,err)
		require.Empty(t,value.Item)
		// Copying a sealed alias into another scope cannot bypass authentication.
		require.NoError(t,adapter.systemService.SaveNativeCompactionMigration(ctx,other.digest(),sealed))
		restarted.nativeMigrations.Remove(other)
		_,err=restarted.loadNativeMigration(ctx,other)
		require.Error(t,err)
	}
	require.NoError(t,adapter.saveNativeMigration(ctx,key,nativeCompactionMigration{RejectedUntil:time.Now().Add(time.Hour)}))
	adapter=newRemoteCompactionAdapter(adapter.requestService,nil,adapter.systemService)
	value,err=adapter.loadNativeMigration(ctx,key)
	require.NoError(t,err)
	require.Empty(t,value.Item)
	require.True(t,time.Now().Before(value.RejectedUntil))
}

func TestNativeCompactionMigrationReplacesOnlyCheckpoint(t *testing.T) {
	body:=[]byte(responsesCompactionHistoryFixture)
	ref,_,_,err:=parseRemoteCompactionRequest(body)
	require.NoError(t,err)
	item:=json.RawMessage(`{"type":"compaction","id":"cmp_destination","encrypted_content":"new-native-ciphertext"}`)
	got,err:=replaceNativeCompactionMigration(body,ref,item)
	require.NoError(t,err)
	for i,prior:=range gjson.GetBytes(body,"input").Array() {
		if i==ref.Index {continue}
		require.JSONEq(t,prior.Raw,gjson.GetBytes(got,"input").Array()[i].Raw)
	}
	for _,field:=range []string{"reasoning","prompt_cache_key","client_metadata","model","store"} {
		require.JSONEq(t,gjson.GetBytes(body,field).Raw,gjson.GetBytes(got,field).Raw)
	}
	for _,response:=range []string{
		`{"status":"incomplete","output":[{"type":"compaction","id":"cmp_target","encrypted_content":"cipher"}]}`,
		`{"status":"completed","output":[{"type":"message","content":[]}]}`,
		`{"status":"completed","output":[{"type":"compaction","id":"cmp_one","encrypted_content":"one"},{"type":"compaction","id":"cmp_two","encrypted_content":"two"}]}`,
	} {
		_,err:=extractNativeCompactionMigration([]byte(response))
		require.ErrorIs(t,err,errNativeCompactionUnavailable)
	}
}
