package orchestrator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const nativeCompactionMigrationHeader = "X-Axonhub-Native-Compaction-Migration"
const nativeCompactionMigrationMaxBytes = 512 << 10

var errNativeCompactionUnavailable = errors.New("native compaction migration returned no unique valid checkpoint")

type nativeCompactionMigration struct {
	Version int `json:"version"`
	Item json.RawMessage `json:"item,omitempty"`
	RejectedUntil time.Time `json:"rejected_until,omitempty"`
}

type nativeCompactionMigrationCacheEntry struct {
	value nativeCompactionMigration
	expires time.Time
}

func nativeCompactionMigrationAAD(key responsesCompactionRecoveryKey) []byte {
	digest := key.digest()
	return []byte("axonhub/native-compaction-migration/v1:"+hex.EncodeToString(digest[:]))
}

func (a *remoteCompactionAdapter) loadNativeMigration(ctx context.Context,key responsesCompactionRecoveryKey)(nativeCompactionMigration,error) {
	if a.nativeMigrations != nil {
		if entry,ok:=a.nativeMigrations.Get(key);ok && time.Now().Before(entry.expires) { return entry.value,nil }
	}
	var value nativeCompactionMigration
	if a.systemService == nil { return value,nil }
	sealed,err:=a.systemService.LoadNativeCompactionMigration(ctx,key.digest())
	if err!=nil { return value,err }
	if sealed!="" {
		aead,err:=a.localCompactionCipher(ctx)
		if err!=nil {return value,err}
		decoded,err:=openCompactionText(aead,nativeCompactionMigrationAAD(key),sealed,nativeCompactionMigrationMaxBytes)
		if err!=nil {return value,err}
		if err:=json.Unmarshal([]byte(decoded),&value);err!=nil {return value,err}
		if value.Version!=1 {return value,errors.New("invalid native compaction migration version")}
		if len(value.Item)>0 {
			if _,err:=nativeMigrationReference(string(value.Item));err!=nil {return value,err}
		}
	}
	if a.nativeMigrations!=nil {a.nativeMigrations.Add(key,nativeCompactionMigrationCacheEntry{value:value,expires:time.Now().Add(time.Minute)})}
	return value,nil
}

func (a *remoteCompactionAdapter) saveNativeMigration(ctx context.Context,key responsesCompactionRecoveryKey,value nativeCompactionMigration) error {
	value.Version=1
	encoded,err:=json.Marshal(value)
	if err!=nil {return err}
	aead,err:=a.localCompactionCipher(ctx)
	if err!=nil {return err}
	sealed,err:=sealCompactionText(aead,nativeCompactionMigrationAAD(key),string(encoded),nativeCompactionMigrationMaxBytes)
	if err!=nil {return err}
	if err:=a.systemService.SaveNativeCompactionMigration(ctx,key.digest(),sealed);err!=nil {return err}
	if a.nativeMigrations!=nil {a.nativeMigrations.Add(key,nativeCompactionMigrationCacheEntry{value:value,expires:time.Now().Add(time.Minute)})}
	return nil
}

// Only an authenticated source with the explicit Codex V2 contract qualifies.
// Legacy standalone compaction has different canonical output semantics.
func nativeMigrationSourceEligible(body []byte,model string) bool {
	metadata:=gjson.GetBytes(body,"client_metadata.x-codex-turn-metadata").String()
	return gjson.GetBytes(body,"model").String()==model &&
		gjson.Get(metadata,"request_kind").String()=="compaction" &&
		gjson.Get(metadata,"compaction.implementation").String()=="responses_compaction_v2"
}

func buildNativeCompactionMigrationRequest(body []byte)([]byte,error) {
	envelope,input,err:=decodeResponsesInput(body)
	if err!=nil {return nil,err}
	trigger:=-1
	for i,item:=range input {
		if rawInputItemType(item)==remoteCompactionTriggerType {
			if trigger>=0 {return nil,errors.New("native migration source has multiple compaction triggers")}
			trigger=i
		}
	}
	if trigger<0 || trigger!=len(input)-1 {return nil,errors.New("native migration requires the exact terminal compaction trigger")}
	envelope["stream"]=json.RawMessage("true")
	envelope["store"]=json.RawMessage("false")
	return json.Marshal(envelope)
}

func extractNativeCompactionMigration(body []byte)(string,error) {
	if gjson.GetBytes(body,"status").String()!="completed" {return "",llm.ErrStreamIncomplete}
	var checkpoint string
	for _,item:=range gjson.GetBytes(body,"output").Array() {
		if item.Get("type").String()!=remoteCompactionItemType {continue}
		if checkpoint!="" {return "",errNativeCompactionUnavailable}
		checkpoint=item.Raw
	}
	if _,err:=nativeMigrationReference(checkpoint);err!=nil {return "",err}
	return checkpoint,nil
}

func nativeMigrationReference(item string)(*remoteCompactionReference,error) {
	if len(item)==0 || len(item)>nativeCompactionMigrationMaxBytes/2 || !gjson.Valid(item) {return nil,errNativeCompactionUnavailable}
	value:=gjson.Parse(item)
	ref:=&remoteCompactionReference{ID:value.Get("id").String(),EncryptedContent:value.Get("encrypted_content").String()}
	if value.Get("type").String()!=remoteCompactionItemType || ref.ID=="" || ref.EncryptedContent=="" || isLocalCompactionReference(ref) {return nil,errNativeCompactionUnavailable}
	return ref,nil
}

func replaceNativeCompactionMigration(body []byte,ref *remoteCompactionReference,item json.RawMessage)([]byte,error) {
	if _,err:=nativeMigrationReference(string(item));err!=nil {return nil,err}
	envelope,input,err:=decodeResponsesInput(body)
	if err!=nil {return nil,err}
	if ref==nil || ref.Index<0 || ref.Index>=len(input) ||
		gjson.GetBytes(input[ref.Index],"id").String()!=ref.ID ||
		gjson.GetBytes(input[ref.Index],"encrypted_content").String()!=ref.EncryptedContent {
		return nil,errors.New("native migration source checkpoint no longer matches")
	}
	input[ref.Index]=item
	if err:=setResponseEnvelopeInput(envelope,input);err!=nil {return nil,err}
	return json.Marshal(envelope)
}

func (a *remoteCompactionAdapter) migrateNativeCompaction(ctx context.Context,key responsesCompactionRecoveryKey,ref *remoteCompactionReference,state *PersistenceState,executor pipeline.Executor)(json.RawMessage,error) {
	if a.systemService==nil {return nil,nil}
	cached,err:=a.loadNativeMigration(ctx,key)
	if err!=nil || len(cached.Item)>0 {return cached.Item,err}
	if time.Now().Before(cached.RejectedUntil) {return nil,nil}
	source,err:=a.loadNativeCompactionSource(ctx,ref,state)
	if err!=nil || source==nil {return nil,err}
	if !nativeMigrationSourceEligible(source.body,key.scope.provider.model) {return nil,nil}
	source,err=bindNativeMigrationDestination(source,state.RawProviderRequest)
	if err!=nil {return nil,err}
	digest:=key.digest()
	result,err:=waitForCompactionFlight(ctx,&a.nativeMigrationGenerator,hex.EncodeToString(digest[:]),func()(any,error){
		cached,err:=a.loadNativeMigration(ctx,key)
		if err!=nil {return nil,err}
		if len(cached.Item)>0 || time.Now().Before(cached.RejectedUntil) {return cached.Item,nil}
		item,err:=a.generateCompaction(ctx,key.cacheKey,source,state,executor,true)
		if err!=nil {
			if !nativeMigrationCanFallback(err) || ctx.Err()!=nil {return nil,err}
			if err:=a.saveNativeMigration(ctx,key,nativeCompactionMigration{RejectedUntil:time.Now().Add(responsesReasoningRecoveryTTL)});err!=nil {return nil,err}
			return json.RawMessage(nil),nil
		}
		generated,err:=nativeMigrationReference(item)
		if err!=nil {return nil,err}
		if remoteCompactionCacheKey(generated)==key.cacheKey {
			return json.RawMessage(nil),a.saveNativeMigration(ctx,key,nativeCompactionMigration{RejectedUntil:time.Now().Add(responsesReasoningRecoveryTTL)})
		}
		value:=nativeCompactionMigration{Item:json.RawMessage(item)}
		persistCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
		defer cancel()
		if err:=a.saveNativeMigration(persistCtx,key,value);err!=nil {return nil,err}
		log.Info(ctx,"retained destination-native compaction migration",log.Int("channel_id",key.scope.provider.channelID),log.String("compaction_id",ref.ID))
		return value.Item,nil
	})
	if err!=nil {return nil,err}
	if result==nil {return nil,nil}
	item,ok:=result.(json.RawMessage)
	if !ok {return nil,fmt.Errorf("invalid native migration result %T",result)}
	return item,nil
}

func bindNativeMigrationDestination(source *remoteCompactionSource,destination *httpclient.Request)(*remoteCompactionSource,error) {
	if destination==nil {return nil,errors.New("native migration requires an effective destination request")}
	metadata:=shared.ReadCodexRequestMetadata(destination.Headers,destination.Body)
	fields:=map[string]any{"session_id":metadata.SessionID,"thread_id":metadata.ThreadID,"window_id":metadata.WindowID}
	for _,key:=range []string{"installation_id","turn_id","root_turn_id","context_window_id","window_number","agent_name"} {
		if value:=gjson.Get(metadata.RawTurnMetadata,key);value.Exists() {fields[key]=value.Value()}
	}
	sourceMetadata:=gjson.GetBytes(source.body,"client_metadata.x-codex-turn-metadata").String()
	rewritten,ok:=rewriteCodexTurnMetadata(sourceMetadata,fields)
	if !ok {return nil,errors.New("native migration source metadata is malformed")}
	headers:=destination.Headers.Clone()
	if headers==nil {headers=make(http.Header)}
	headers.Del("Authorization")
	headers.Del("Content-Length")
	headers.Del(codexTurnStateHeader)
	headers.Set(codexTurnMetadataHeader,rewritten)
	body:=source.body
	patches:=map[string]string{
		"client_metadata.session_id":metadata.SessionID,"client_metadata.thread_id":metadata.ThreadID,
		"client_metadata.x-codex-window-id":metadata.WindowID,"client_metadata.x-codex-turn-metadata":rewritten,
	}
	if cacheKey:=gjson.GetBytes(destination.Body,"prompt_cache_key");cacheKey.Exists() {
		patches["prompt_cache_key"]=cacheKey.String()
	} else {
		var err error
		body,err=sjson.DeleteBytes(body,"prompt_cache_key")
		if err!=nil {return nil,err}
	}
	for path,value:=range patches {
		var err error
		body,err=sjson.SetBytes(body,path,value)
		if err!=nil {return nil,err}
	}
	return &remoteCompactionSource{body:body,headers:headers},nil
}

func nativeMigrationCanFallback(err error) bool {
	if errors.Is(err,errNativeCompactionUnavailable) {return true}
	if _,ok:=errors.AsType[*compactionSourceRejectedError](err);ok {return true}
	code,message,param,ok:=responsesBadRequestDetails(err)
	if !ok {return false}
	// Explicit protocol rejection only; authentication, overload, cancellation,
	// unrelated validation, and partial stream failures must retain their cause.
	return code=="unsupported_compaction" ||
		(code=="unknown_parameter" || code=="unsupported_parameter") && param=="compaction_trigger" ||
		code=="invalid_value" && message=="Unsupported input item type: compaction_trigger"
}

