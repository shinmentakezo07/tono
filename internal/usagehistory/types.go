package usagehistory

import "time"

// JSONLRecord is the structure written to each line of a usage history JSONL file.
// Field names match the redisqueue plugin's JSON output for frontend compatibility.
type JSONLRecord struct {
	EventID         string     `json:"event_id"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	Alias           string     `json:"alias"`
	Endpoint        string     `json:"endpoint"`
	AuthType        string     `json:"auth_type"`
	APIKey          string     `json:"api_key"`
	RequestID       string     `json:"request_id"`
	ReasoningEffort string     `json:"reasoning_effort"`
	Timestamp       time.Time  `json:"timestamp"`
	LatencyMs       int64      `json:"latency_ms"`
	Source          string     `json:"source"`
	AuthIndex       string     `json:"auth_index"`
	Tokens          TokenStats `json:"tokens"`
	Failed          bool       `json:"failed"`
	Fail            FailDetail `json:"fail"`
}

// TokenStats mirrors redisqueue.tokenStats.
type TokenStats struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// FailDetail mirrors redisqueue.failDetail.
type FailDetail struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}
