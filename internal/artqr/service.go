package artqr

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
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
	CustomPrompt   string
	Placement      model.Placement
}

func (s *Service) AnalyzeStyle(ctx context.Context, refImgBytes []byte, placement model.Placement) (*vision.StyleAnalysisResult, error) {
	return s.analyzer.AnalyzeStyle(ctx, refImgBytes, placement)
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
		Prompt:              params.CustomPrompt,
		Placement:           params.Placement,
		MaxAttempts:         4,
		Attempts:            0,
		ControlCanvasPNG:    controlCanvas,
		SourceQRPNG:         decoded.PNGBytes,
		ReferenceImageJPEG:  params.ReferenceBytes,
		Images:              make([]model.OutputImage, 0),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	// Launch async execution
	go s.processJob(job)

	return job, nil
}

func (s *Service) processJob(job *model.ArtQRJob) {
	s.workerSem <- struct{}{}
	defer func() { <-s.workerSem }()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	job.UpdateStatus("processing", 15)

	// Step A: Style Analysis (if reference image is provided)
	var analysis *vision.StyleAnalysisResult
	var preset *model.ArtQRPreset

	if len(job.ReferenceImageJPEG) > 0 {
		job.UpdateStatus("analyzing_style", 20)
		var err error
		analysis, err = s.analyzer.AnalyzeStyle(ctx, job.ReferenceImageJPEG, job.Placement)
		if err != nil || analysis == nil {
			log.Printf("[Vision Notice] Vision API unavailable (%v), using smart local visual prompt fallback", err)
			analysis = &vision.StyleAnalysisResult{
				Style:           "Chân dung nghệ thuật / Tác phẩm gốc",
				Palette:         []string{"#8b0000", "#ffd700", "#1a202c", "#f5d0a9"},
				Lighting:        "Ánh sáng studio cinematic",
				Texture:         "Vân vải & chi tiết tự nhiên",
				GeneratedPrompt: "Masterpiece high quality artwork preserving the exact subject, clothing, textures, and background of the image with the QR code seamlessly integrated into natural folds and shadows",
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
	if strings.TrimSpace(job.Prompt) != "" {
		finalPrompt = strings.TrimSpace(job.Prompt)
	}
	job.Prompt = finalPrompt
	job.NegativePrompt = negativePrompt

	// Step C: Adaptive conditioning search (1.10 -> 1.50)
	conditioningScales := []float64{1.10, 1.20, 1.30, 1.40, 1.50}
	job.MaxAttempts = len(conditioningScales)
	targetOutputs := 1

	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		job.IncrementAttempt()
		needed := targetOutputs - len(job.Images)
		if needed <= 0 {
			break
		}

		currentProgress := 25 + int(float64(attempt)/float64(job.MaxAttempts)*65)
		if currentProgress > 90 {
			currentProgress = 90
		}
		job.UpdateStatus("generating", currentProgress)

		scaleIdx := attempt - 1
		if scaleIdx >= len(conditioningScales) {
			scaleIdx = len(conditioningScales) - 1
		}
		currentScale := conditioningScales[scaleIdx]
		seed := int(time.Now().UnixNano()&0x7fffffff) + rand.Intn(10000)

		qrControlBytes := job.ControlCanvasPNG
		if len(job.ReferenceImageJPEG) > 0 && len(job.SourceQRPNG) > 0 {
			qrControlBytes = job.SourceQRPNG
		}

		req := &provider.GenerationRequest{
			Payload:             job.OriginalPayload,
			Prompt:              finalPrompt,
			NegativePrompt:      negativePrompt,
			QRControlImagePNG:   qrControlBytes,
			ReferenceImageBytes: job.ReferenceImageJPEG,
			Placement:           job.Placement,
			ConditioningScale:   currentScale,
			ReferenceStrength:   0.72,
			GuidanceScale:       7.5,
			Seed:                seed,
			Width:               1024,
			Height:              1024,
			NumOutputs:          needed,
		}

		// Raw AI diffusion execution
		candidates, err := s.provider.Generate(ctx, req)
		if err != nil {
			if attempt == job.MaxAttempts && len(job.Images) == 0 {
				job.SetError("Không thể tạo ảnh từ AI Provider: " + err.Error())
				return
			}
			continue
		}

		job.UpdateStatus("validating", currentProgress+3)

		// Step D: Validate raw candidate diffusion output directly
		// Flow: cand.PNGBytes -> ValidateGeneratedQR -> valid: return, invalid: retry with next conditioning scale
		for _, cand := range candidates {
			vResult := qr.ValidateGeneratedQR(cand.PNGBytes, job.OriginalPayload)
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

		if len(job.Images) >= targetOutputs {
			break
		}
	}

	if len(job.Images) > 0 {
		job.UpdateStatus("completed", 100)
	} else {
		job.SetError("Không thể tạo Art QR quét ổn định với phong cách này sau các lần thử. Hãy thử lại hoặc chọn phong cách khác.")
	}
}
