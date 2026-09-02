package model

import (
	"sync"
	"time"
)

type OutputImage struct {
	URL                string  `json:"url" bson:"url"`
	Verified           bool    `json:"verified" bson:"verified"`
	DecodedPayloadHash string  `json:"decoded_payload_hash,omitempty" bson:"decoded_payload_hash,omitempty"`
	Seed               int     `json:"seed,omitempty" bson:"seed,omitempty"`
	ConditioningScale  float64 `json:"conditioning_scale,omitempty" bson:"conditioning_scale,omitempty"`
}

type ArtQRJob struct {
	mu                  sync.RWMutex
	ID                  string        `json:"job_id" bson:"_id"`
	UserID              string        `json:"user_id,omitempty" bson:"user_id,omitempty"`
	Status              string        `json:"status" bson:"status"` // queued, decoding, analyzing_style, generating, validating, retrying, completed, failed
	Progress            int           `json:"progress" bson:"progress"`
	OriginalPayloadHash string        `json:"original_payload_hash" bson:"original_payload_hash"`
	OriginalPayload     string        `json:"-" bson:"original_payload"`
	PresetID            string        `json:"preset_id,omitempty" bson:"preset_id,omitempty"`
	Placement           Placement     `json:"placement" bson:"placement"`
	Prompt              string        `json:"prompt,omitempty" bson:"prompt,omitempty"`
	NegativePrompt      string        `json:"negative_prompt,omitempty" bson:"negative_prompt,omitempty"`
	Provider            string        `json:"provider,omitempty" bson:"provider,omitempty"`
	Attempts            int           `json:"attempts" bson:"attempts"`
	MaxAttempts         int           `json:"max_attempts" bson:"max_attempts"`
	RejectedCount       int           `json:"rejected_count" bson:"rejected_count"`
	Images              []OutputImage `json:"images" bson:"images"`
	Error               string        `json:"error,omitempty" bson:"error,omitempty"`
	CreatedAt           time.Time     `json:"created_at" bson:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at" bson:"updated_at"`

	// In-memory binary caches during generation
	SourceQRPNG        []byte `json:"-" bson:"-"`
	ControlCanvasPNG   []byte `json:"-" bson:"-"`
	ReferenceImageJPEG []byte `json:"-" bson:"-"`
}

func (j *ArtQRJob) UpdateStatus(status string, progress int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	j.Progress = progress
	j.UpdatedAt = time.Now()
}

func (j *ArtQRJob) SetError(err string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "failed"
	j.Error = err
	j.UpdatedAt = time.Now()
}

func (j *ArtQRJob) AddOutput(img OutputImage) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Images = append(j.Images, img)
	j.UpdatedAt = time.Now()
}

func (j *ArtQRJob) IncrementRejected() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.RejectedCount++
	j.UpdatedAt = time.Now()
}

func (j *ArtQRJob) IncrementAttempt() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Attempts++
	j.UpdatedAt = time.Now()
}

func (j *ArtQRJob) Snapshot() ArtQRJob {
	j.mu.RLock()
	defer j.mu.RUnlock()
	copied := *j
	copied.Images = make([]OutputImage, len(j.Images))
	copy(copied.Images, j.Images)
	return copied
}
