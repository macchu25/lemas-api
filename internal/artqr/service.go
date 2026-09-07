package artqr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/prompt"
	"xkiro-backend/internal/artqr/provider"
	"xkiro-backend/internal/artqr/qr"
	"xkiro-backend/internal/artqr/vision"
)

const jobsPersistenceFile = "artqr_jobs.json"

type Service struct {
	mu        sync.RWMutex
	jobs      map[string]*model.ArtQRJob
	presets   map[string]model.ArtQRPreset
	analyzer  vision.StyleAnalyzer
	provider  provider.ArtQRProvider
	workerSem chan struct{}
}

func (s *Service) saveJobs() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	persisted := make(map[string]model.ArtQRJob, len(s.jobs))
	for id, j := range s.jobs {
		persisted[id] = j.Snapshot()
	}

	data, err := json.Marshal(persisted)
	if err == nil {
		_ = os.WriteFile(jobsPersistenceFile, data, 0644)
	}
}

func (s *Service) loadJobs() {
	data, err := os.ReadFile(jobsPersistenceFile)
	if err != nil {
		return
	}
	var loaded map[string]model.ArtQRJob
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range loaded {
		copyJob := j
		s.jobs[id] = &copyJob
	}
	log.Printf("[ArtQR] Restored %d jobs from %s", len(s.jobs), jobsPersistenceFile)
}

func NewService() *Service {
	p := provider.NewHuggingFaceProvider()
	v := vision.NewXKiroVisionAnalyzer()

	presetMap := make(map[string]model.ArtQRPreset)
	for _, pr := range prompt.DefaultPresets {
		presetMap[pr.ID] = pr
		presetMap[pr.Slug] = pr
	}

	svc := &Service{
		jobs:      make(map[string]*model.ArtQRJob),
		presets:   presetMap,
		analyzer:  v,
		provider:  p,
		workerSem: make(chan struct{}, 4),
	}
	svc.loadJobs()
	return svc
}

func (s *Service) ListPresets() []model.ArtQRPreset {
	return prompt.DefaultPresets
}

func (s *Service) GetPresets() []model.ArtQRPreset {
	return prompt.DefaultPresets
}

func (s *Service) GetPreset(idOrSlug string) (*model.ArtQRPreset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.presets[idOrSlug]
	if !ok {
		return nil, false
	}
	return &p, true
}

func (s *Service) GetJob(jobID string) (*model.ArtQRJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[jobID]
	if !ok {
		return nil, false
	}
	return j, true
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
		if params.PresetID != "" {
			if p, ok := s.GetPreset(params.PresetID); ok && p != nil && p.Placement != nil && p.Placement.IsValid() {
				params.Placement = *p.Placement
			} else {
				params.Placement = model.DefaultPlacement()
			}
		} else {
			params.Placement = model.DefaultPlacement()
		}
	}

	// Smart Portrait Face Protection: If reference photo is vertical/portrait (height > width)
	// and placement is at the upper half/face/neck (Y < 0.58),
	// automatically reposition placement to chest/uniform fabric (x: 0.23, y: 0.61, size: 0.38) to keep face 100% pristine
	if len(params.ReferenceBytes) > 0 && params.Placement.Y < 0.58 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(params.ReferenceBytes)); err == nil {
			if cfg.Height > int(float64(cfg.Width)*1.05) {
				params.Placement = model.Placement{X: 0.23, Y: 0.61, Size: 0.38}
				log.Printf("[ArtQR] Smart Portrait Guard: Automatically protected subject face by moving QR placement to chest (0.23, 0.61, 0.38)")
			}
		}
	}

	// 1. Decode original QR
	decoded, err := qr.DecodeQRCode(params.QRPNGBytes)
	if err != nil {
		return nil, fmt.Errorf("không thể giải mã QR: %w", err)
	}

	// 2. Generate clean QR code (Level H) with standard quiet zone border
	sourceQRPNG := decoded.PNGBytes
	if cleanQR, qErr := qrcode.New(decoded.Payload, qrcode.Highest); qErr == nil {
		cleanQR.DisableBorder = false
		if cleanBytes, err := cleanQR.PNG(512); err == nil && len(cleanBytes) > 0 {
			sourceQRPNG = cleanBytes
		}
	}

	// 3. Build 1024x1024 placement control canvas
	controlCanvas, err := qr.BuildControlCanvas(sourceQRPNG, params.Placement, 1024)
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
		SourceQRPNG:         sourceQRPNG,
		ReferenceImageJPEG:  params.ReferenceBytes,
		Images:              make([]model.OutputImage, 0),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()
	s.saveJobs()

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
				GeneratedPrompt: "Masterpiece high quality artwork preserving the exact subject, clothing, textures, and background with ornate golden bullion embroidery cords and brass medals",
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

	// Step C: Adaptive conditioning search starting at optimal scannable scale
	conditioningScales := []float64{1.70, 1.64, 1.76}
	if len(job.ReferenceImageJPEG) == 0 && preset != nil && preset.ConditioningScale > 0 {
		baseScale := preset.ConditioningScale
		conditioningScales = []float64{baseScale, baseScale + 0.03, baseScale - 0.03}
	}
	job.MaxAttempts = len(conditioningScales)
	targetOutputs := 1

	log.Printf("[ArtQR] [%s] Starting job execution: max_attempts=%d, placement=(%.2f, %.2f, %.2f)",
		job.ID, job.MaxAttempts, job.Placement.X, job.Placement.Y, job.Placement.Size)

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
		seed := 42
		if attempt > 1 {
			seed = int(time.Now().UnixNano()&0x7fffffff) + rand.Intn(10000)
		}

		log.Printf("[ArtQR] [%s] Attempt %d/%d: conditioning_scale=%.2f, seed=%d",
			job.ID, attempt, job.MaxAttempts, currentScale, seed)

		qrControlBytes := job.SourceQRPNG
		if len(qrControlBytes) == 0 {
			qrControlBytes = job.ControlCanvasPNG
		}

		req := &provider.GenerationRequest{
			Payload:             job.OriginalPayload,
			Prompt:              finalPrompt,
			NegativePrompt:      negativePrompt,
			QRControlImagePNG:   qrControlBytes,
			ReferenceImageBytes: job.ReferenceImageJPEG,
			Placement:           job.Placement,
			ConditioningScale:   currentScale,
			ReferenceStrength:   0.74,
			GuidanceScale:       7.5,
			Seed:                seed,
			Width:               1024,
			Height:              1024,
			NumOutputs:          4,
		}

		// Raw AI diffusion execution
		candidates, err := s.provider.Generate(ctx, req)
		if err != nil {
			log.Printf("[ArtQR] [%s] Attempt %d AI provider error: %v", job.ID, attempt, err)
			if attempt == job.MaxAttempts && len(job.Images) == 0 {
				job.SetError("Không thể tạo ảnh từ AI Provider: " + err.Error())
				return
			}
			continue
		}

		job.UpdateStatus("validating", currentProgress+3)

		// Step D: Validate candidate
		for _, cand := range candidates {
			vResult := qr.ValidateGeneratedQRWithPlacement(cand.PNGBytes, job.OriginalPayload, job.Placement)
			matchesHash := (vResult.PayloadHash != "" && vResult.PayloadHash == job.OriginalPayloadHash)
			isVerified := vResult.Valid || matchesHash

			log.Printf("[ArtQR] [%s] Attempt %d validation result: valid=%v, payloadMatch=%v (decoded=%q)",
				job.ID, attempt, isVerified, matchesHash, vResult.DecodedPayload)

			if isVerified {
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
		log.Printf("[ArtQR] [%s] Job COMPLETED successfully with %d images", job.ID, len(job.Images))
	} else {
		job.SetError("Không thể tạo Art QR sau các lần thử. Hãy thử lại hoặc chọn phong cách khác.")
		log.Printf("[ArtQR] [%s] Job FAILED: %s", job.ID, job.Error)
	}
	s.saveJobs()
}
