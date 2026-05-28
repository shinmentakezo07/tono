package usagehistory

import "time"

// PgRecord is the flat structure stored in the TimescaleDB hypertable.
// Field names match the SQL column names exactly.
type PgRecord struct {
	CreatedAt           time.Time
	Provider            string
	Model               string
	Alias               string
	Endpoint            string
	AuthType            string
	APIKey              string
	RequestID           string
	ReasoningEffort     string
	LatencyMs           int64
	Source              string
	AuthIndex           string
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	Failed              bool
	FailStatusCode      int
	FailBody            string
}

// toJSONLRecord converts a PgRecord to a JSONLRecord for API response compatibility.
func (r *PgRecord) toJSONLRecord() JSONLRecord {
	return JSONLRecord{
		Provider:        r.Provider,
		Model:           r.Model,
		Alias:           r.Alias,
		Endpoint:        r.Endpoint,
		AuthType:        r.AuthType,
		APIKey:          r.APIKey,
		RequestID:       r.RequestID,
		ReasoningEffort: r.ReasoningEffort,
		Timestamp:       r.CreatedAt,
		LatencyMs:       r.LatencyMs,
		Source:          r.Source,
		AuthIndex:       r.AuthIndex,
		Tokens: TokenStats{
			InputTokens:         r.InputTokens,
			OutputTokens:        r.OutputTokens,
			ReasoningTokens:     r.ReasoningTokens,
			CachedTokens:        r.CachedTokens,
			CacheReadTokens:     r.CacheReadTokens,
			CacheCreationTokens: r.CacheCreationTokens,
			TotalTokens:         r.TotalTokens,
		},
		Failed: r.Failed,
		Fail: FailDetail{
			StatusCode: r.FailStatusCode,
			Body:       r.FailBody,
		},
	}
}

// fromJSONLRecord converts a JSONLRecord to a PgRecord for DB insertion.
func fromJSONLRecord(r *JSONLRecord) PgRecord {
	return PgRecord{
		CreatedAt:           r.Timestamp,
		Provider:            r.Provider,
		Model:               r.Model,
		Alias:               r.Alias,
		Endpoint:            r.Endpoint,
		AuthType:            r.AuthType,
		APIKey:              r.APIKey,
		RequestID:           r.RequestID,
		ReasoningEffort:     r.ReasoningEffort,
		LatencyMs:           r.LatencyMs,
		Source:              r.Source,
		AuthIndex:           r.AuthIndex,
		InputTokens:         r.Tokens.InputTokens,
		OutputTokens:        r.Tokens.OutputTokens,
		ReasoningTokens:     r.Tokens.ReasoningTokens,
		CachedTokens:        r.Tokens.CachedTokens,
		CacheReadTokens:     r.Tokens.CacheReadTokens,
		CacheCreationTokens: r.Tokens.CacheCreationTokens,
		TotalTokens:         r.Tokens.TotalTokens,
		Failed:              r.Failed,
		FailStatusCode:      r.Fail.StatusCode,
		FailBody:            r.Fail.Body,
	}
}
