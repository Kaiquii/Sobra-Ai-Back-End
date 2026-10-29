package assistant

// Only fixed parser reasons may be logged. Neither the failed plan nor a
// decoder/provider error can become a log field, even after future changes.
func financialQueryRejectionReason(err error) string {
	if err == nil {
		return "unknown"
	}
	switch err.Error() {
	case "invalid query":
		return "encoding_or_size"
	case "invalid query shape", "invalid query object":
		return "shape"
	case "null query field":
		return "null_field"
	case "invalid periods", "missing or null months":
		return "period_shape"
	case "invalid category":
		return "category"
	case "invalid payment source":
		return "payment_source"
	case "invalid grouping":
		return "grouping"
	case "invalid operation":
		return "operation"
	case "invalid query bounds":
		return "bounds"
	case "invalid ranking position":
		return "ranking_position"
	case "review requires categories":
		return "review_grouping"
	case "budget requires a category and months":
		return "budget_shape"
	case "invalid period", "invalid month", "too many years":
		return "period_bounds"
	default:
		return "unknown"
	}
}
