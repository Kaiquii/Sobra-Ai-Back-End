package assistant

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestFinancialQueryRejectionsLogOnlyClosedReasons(t *testing.T) {
	const private = "PRIVATE_QUERY_SENTINEL_3947"
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, test := range []struct {
		body, reason string
	}{
		{`{"periods":[{"year":2034,"months":[2]}],"category_name":"` + private + `","group_by":"month","operation":"budget","limit":null}`, "null_field"},
		{`{"periods":[{"year":2034,"months":[2]}],"payment_source":"` + private + `","group_by":"month","operation":"sum"}`, "payment_source"},
		{`{"periods":[{"year":2034,"months":[2]}],"group_by":"month","operation":"sum","` + private + `":99999}`, "shape"},
	} {
		output.Reset()
		query := financialQueryTestRaw(t, test.body)
		_, reply, _, raw := executeFinancialQueryDB(context.Background(), nil, 7, query, assistantNow())
		result := raw.(map[string]any)
		if result["status"] != "needs_clarification" || result["reason"] != "invalid_query" || strings.Contains(reply, private) {
			t.Fatalf("diagnostic changed the public clarification contract: %+v", result)
		}
		if !strings.Contains(output.String(), "Assistant financial_query rejected reason="+test.reason) || strings.Contains(output.String(), private) || strings.Contains(output.String(), "99999") {
			t.Fatal("query rejection log exposed content or lost its closed diagnostic")
		}
	}
	if financialQueryRejectionReason(errors.New(private)) != "unknown" || financialQueryRejectionReason(nil) != "unknown" {
		t.Fatal("unknown parser/provider content can escape into the diagnostic enum")
	}
}
