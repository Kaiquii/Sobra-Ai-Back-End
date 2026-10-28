package assistant

import "context"

type textGenerator interface {
	generateText(req GeminiRequest) (string, error)
}

func newAssistantGenerator() (textGenerator, error) {
	return newAssistantGeneratorForModel(context.Background(), "")
}
