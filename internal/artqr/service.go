package artqr

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/prompt"
	"xkiro-backend/internal/artqr/provider"
	"xkiro-backend/internal/artqr/qr"
	"xkiro-backend/internal/artqr/vision"
)

type Service struct {
	mu        sync.RWMutex
	jobs      map[string]*model.ArtQRJob
	presets   map[string]model.ArtQRPreset
	analyzer  vision.StyleAnalyzer
	provider  provider.ArtQRProvider
	workerSem chan struct{}
}

func NewService() *Service {
	presetsMap := make(map[string]model.ArtQRPreset)
	for _, p := range prompt.DefaultPresets {
		presetsMap[p.ID] = p
		presetsMap[p.Slug] = p
	}

	return &Service{
		jobs:      make(map[string]*model.ArtQRJob),
		presets:   presetsMap,
		analyzer:  vision.NewXKiroVisionAnalyzer(),
		provider:  provider.NewHuggingFaceProvider(),
		workerSem: make(chan struct{}, 3),
	}
}

func (s *Service) GetPresets() []model.ArtQRPreset {
	return prompt.DefaultPresets
}

func (s *Service) GetPreset(id string) (*model.ArtQRPreset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.presets[id]
	if !ok {
		return nil, false
	}
	return &p, true
}

func (s *Service) GetJob(jobID string) (*model.ArtQRJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, exists := s.jobs[jobID]
	if !exists {
		return nil, false
	}
	snap := job.Snapshot()
	return &snap, true
}

type CreateJobParams struct {
	UserID         string
	QRPNGBytes     []byte
	ReferenceBytes []byte
	PresetID       string
	Placement      model.Placement
}

func (s *Service) CreateJob(ctx context.Context, params CreateJobParams) (*model.ArtQRJob, error) {
	if len(params.QRPNGBytes) == 0 {
		return nil, errors.New("qr_image is required")
	}

	if !params.Placement.IsValid() {
		params.Placement = model.DefaultPlacement()
	}

	// 1. Decode original QR
	decoded, err := qr.DecodeQRCode(params.QRPNGBytes)
	if err != nil {
		return nil, fmt.Errorf("không thể giải mã QR: %w", err)
	}

	// 2. Build 1024x1024 placement control canvas
	controlCanvas, err := qr.BuildControlCanvas(decoded.PNGBytes, params.Placement, 1024)
	if err != nil {
		return nil, fmt.Errorf("không thể khởi tạo vùng định vị QR: %w", err)
	}

	jobID := "artqr_" + uuid.New().String()
	now := time.Now()

	job := &model.ArtQRJob{
		ID:                  jobID,
		UserID:              params.UserID,
		Status:              "queued",
		Progress:            5,
		OriginalPayload:     decoded.Payload,
		OriginalPayloadHash: decoded.PayloadHash,
		PresetID:            params.PresetID,
		Placement:           params.Placement,
		MaxAttempts:         4,
		Images:              []model.OutputImage{},
		CreatedAt:           now,
		UpdatedAt:           now,
		SourceQRPNG:         decoded.PNGBytes,
		ControlCanvasPNG:    controlCanvas,
		ReferenceImageJPEG:  params.ReferenceBytes,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	// Launch async execution
	go s.processJob(job)

	snap := job.Snapshot()
	return &snap, nil
}

func (s *Service) processJob(job *model.ArtQRJob) {
	s.workerSem <- struct{}{}
	defer func() { <-s.workerSem }()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// Step A: Style Analysis (if reference image is provided)
	var analysis *vision.StyleAnalysisResult
	var preset *model.ArtQRPreset

	if len(job.ReferenceImageJPEG) > 0 {
		job.UpdateStatus("analyzing_style", 20)
		var err error
		analysis, err = s.analyzer.AnalyzeStyle(ctx, job.ReferenceImageJPEG, job.Placement)
		if err != nil {
			// Non-blocking fallback if vision model is busy
			analysis = &vision.StyleAnalysisResult{
				Style:           "expressive modern artwork",
				Palette:         []string{"cobalt", "emerald", "gold"},
				GeneratedPrompt: "dynamic expressive artwork with rich textures and balanced contrasts",
			}
		}
	} else if job.PresetID != "" {
		if p, ok := s.GetPreset(job.PresetID); ok {
			preset = p
		}
	}

	if preset == nil && analysis == nil {
		preset = &prompt.DefaultPresets[0]
	}

	// Step B: Build placement-aware prompt
	finalPrompt, negativePrompt := prompt.BuildPrompt(preset, analysis, job.Placement)
	job.Prompt = finalPrompt
	job.NegativePrompt = negativePrompt

	baseScale := 1.30
	if preset != nil && preset.ConditioningScale > 0 {
		baseScale = preset.ConditioningScale
	}

	// Step C: Execution & Adaptive Retry Loop
	targetOutputs := 4
	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		job.IncrementAttempt()
		needed := targetOutputs - len(job.Images)
		if needed <= 0 {
			break
		}

		currentProgress := 30 + (attempt * 15)
		if currentProgress > 85 {
			currentProgress = 85
		}
		job.UpdateStatus("generating", currentProgress)

		// Adaptive scale step
		currentScale := baseScale + (float64(attempt-1) * 0.08)
		seed := int(time.Now().UnixNano()&0x7fffffff) + rand.Intn(10000)

		req := &provider.GenerationRequest{
			Payload:           job.OriginalPayload,
			Prompt:            finalPrompt,
			NegativePrompt:    negativePrompt,
			QRControlImagePNG: job.ControlCanvasPNG,
			ConditioningScale: currentScale,
			GuidanceScale:     7.5,
			Seed:              seed,
			Width:             1024,
			Height:            1024,
			NumOutputs:        needed,
		}

		candidates, err := s.provider.Generate(ctx, req)
		if err != nil {
			if attempt == job.MaxAttempts && len(job.Images) == 0 {
				job.SetError("Không thể tạo ảnh từ AI Provider: " + err.Error())
				return
			}
			continue
		}

		job.UpdateStatus("validating", currentProgress+5)

		// Step D: Strict QR Validation
		for _, cand := range candidates {
			vResult := qr.ValidateGeneratedQR(cand.PNGBytes, job.OriginalPayload)

			// We record verified output
			if vResult.Valid {
				job.AddOutput(model.OutputImage{
					URL:                cand.URL,
					Verified:           true,
					DecodedPayloadHash: vResult.PayloadHash,
					Seed:               cand.Seed,
					ConditioningScale:  currentScale,
				})
			} else {
				job.IncrementRejected()

			}

			if len(job.Images) >= targetOutputs {
				break
			}
		}
	}

	if len(job.Images) > 0 {
		job.UpdateStatus("completed", 100)
	} else {
		job.SetError("Không thể tạo Art QR quét ổn định với phong cách này sau các lần thử. Hãy thử lại hoặc chọn phong cách khác.")
	}
}
