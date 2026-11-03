package assistant

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAssistantTimeoutReplyDoesNotBlameTheQuestionOrExposeDetails(t *testing.T) {
	const private = "PRIVATE_TRANSPORT_SENTINEL_4392"
	err := fmt.Errorf(private+": %w", OpenRouterError{Code: "assistant_request_timeout", StatusCode: http.StatusGatewayTimeout, RetryAfterSeconds: 15})
	reply, code, retry := assistantErrorResponse(err)
	if code != "assistant_request_timeout" || retry != 15 || !strings.Contains(reply, "demorou") || !strings.Contains(reply, "Nenhum lancamento foi alterado") {
		t.Fatalf("timeout response lost its cause or retry contract: %q %q %d", reply, code, retry)
	}
	for _, forbidden := range []string{private, "periodo", "comparar", "resposta valida"} {
		if strings.Contains(reply, forbidden) {
			t.Fatalf("timeout was confused with invalid interpretation or exposed details: %q", reply)
		}
	}
	for _, oldCode := range []string{"assistant_invalid_response", "assistant_request_cancelled", "assistant_quota_exceeded", "assistant_user_limit"} {
		reply, code, retry := assistantErrorResponse(OpenRouterError{Code: oldCode, RetryAfterSeconds: 17})
		if code != oldCode || retry != 17 || strings.Contains(reply, "demorou alem do tempo de espera") {
			t.Fatalf("timeout handling changed an unrelated error: %q %q %d", reply, code, retry)
		}
	}
}
