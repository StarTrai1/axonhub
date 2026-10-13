package api

import (
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "testing/iotest"

 "github.com/gin-gonic/gin"
 "github.com/stretchr/testify/require"

 "github.com/looplj/axonhub/internal/server/orchestrator"
 "github.com/looplj/axonhub/llm/transformer"
 "github.com/looplj/axonhub/llm/transformer/anthropic"
 "github.com/looplj/axonhub/llm/transformer/doubao"
 "github.com/looplj/axonhub/llm/transformer/gemini"
 "github.com/looplj/axonhub/llm/transformer/openai"
 "github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestClientBodyFailureStopsBeforeProvider(t *testing.T) {
 handlers := func(in transformer.Inbound) *ChatCompletionHandlers {
  return NewChatCompletionHandlers(&orchestrator.ChatCompletionOrchestrator{Inbound: in})
 }
 speech := &OpenAIHandlers{SpeechHandlers: handlers(openai.NewSpeechInboundTransformer())}
 video := &OpenAIHandlers{VideoHandlers: handlers(openai.NewVideoInboundTransformer())}
 task := &DoubaoHandlers{CreateOrchestrator: &orchestrator.ChatCompletionOrchestrator{Inbound: doubao.NewVideoInboundTransformer()}}
 routes := []struct{path string; handler gin.HandlerFunc}{
  {"/v1/chat/completions", handlers(openai.NewInboundTransformer()).ChatCompletion},
  {"/v1/responses", handlers(responses.NewInboundTransformer()).ChatCompletion},
  {"/v1/images/edits", handlers(openai.NewImageEditInboundTransformer()).ChatCompletion},
  {"/v1/audio/transcriptions", handlers(openai.NewTranscriptionInboundTransformer()).ChatCompletion},
  {"/v1/audio/translations", handlers(openai.NewTranslationInboundTransformer()).ChatCompletion},
  {"/v1/messages", handlers(anthropic.NewInboundTransformer()).ChatCompletion},
  {"/v1beta/models/gemini:generateContent", handlers(gemini.NewInboundTransformer()).ChatCompletion},
  {"/v1/audio/speech", speech.CreateSpeech},
  {"/v1/videos", video.CreateVideo},
  {"/playground/chat/completions", (&PlaygroundHandlers{}).ChatCompletion},
  {"/api/v3/contents/generations/tasks", task.CreateTask},
 }
 for _, route := range routes {
  for _, tc := range []struct{name string; failure error; status int; message string}{
   {"interrupted upload", io.ErrUnexpectedEOF, 400, "unexpected EOF"},
   {"body limit", &http.MaxBytesError{Limit: 32}, 413, "request body too large"},
  } {
   t.Run(route.path+"/"+tc.name,func(t *testing.T){
    router := gin.New()
    router.POST(route.path,route.handler)
    request := httptest.NewRequest(http.MethodPost,route.path,io.MultiReader(strings.NewReader(`{"model":"fixture"`),iotest.ErrReader(tc.failure)))
    request.Header.Set("Content-Type","application/json")
    recorder := httptest.NewRecorder()
    router.ServeHTTP(recorder,request)
    require.Equal(t,tc.status,recorder.Code)
    require.Contains(t,recorder.Body.String(),tc.message)
    require.NotContains(t,recorder.Body.String(),"internal_server_error")
   })
  }
 }
}

func TestClientBodyMalformedCompressionIsBadRequest(t *testing.T) {
 router := gin.New()
 handlers := NewChatCompletionHandlers(&orchestrator.ChatCompletionOrchestrator{Inbound: responses.NewInboundTransformer()})
 router.POST("/v1/responses",handlers.ChatCompletion)
 request := httptest.NewRequest(http.MethodPost,"/v1/responses",strings.NewReader("not a gzip document"))
 request.Header.Set("Content-Encoding","gzip")
 recorder := httptest.NewRecorder()
 router.ServeHTTP(recorder,request)
 require.Equal(t,400,recorder.Code)
 require.Contains(t,recorder.Body.String(),"gzip")
}
