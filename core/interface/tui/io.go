package tui

import (
	"context"
	"reflect"
	"unsafe"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/genai"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func (m model) runAgentStream(text string) {
	ctx := context.Background()
	userMsg := genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	}
	runIter := m.runner.Run(ctx, "tui", m.sessionID, &userMsg, adkagent.RunConfig{})

	for event, err := range runIter {
		if err != nil {
			program.Send(responseErrMsg{err: err})
			return
		}
		if event == nil {
			continue
		}
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					program.Send(responseChunkMsg(part.Text))
				}
				if part.FunctionCall != nil {
					program.Send(toolCallMsg(part.FunctionCall.Name))
				}
			}
		}
	}
	program.Send(responseDoneMsg{})
}

// silenceGormLogger reaches into the ADK database session service via unsafe
// reflection to mute its GORM logger, since it writes to stdout and would
// otherwise corrupt the Bubble Tea alt-screen rendering.
func silenceGormLogger(service interface{}) {
	val := reflect.ValueOf(service)
	if val.Kind() != reflect.Ptr {
		return
	}
	val = val.Elem()
	if val.Type().Name() != "databaseService" {
		return
	}
	dbField := val.FieldByName("db")
	if !dbField.IsValid() {
		return
	}

	ptr := unsafe.Pointer(dbField.UnsafeAddr())
	gormDB := *(**gorm.DB)(ptr)
	if gormDB != nil {
		gormDB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	}
}
