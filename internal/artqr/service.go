package artqr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/google/uuid"
	"xkiro-backend/internal/artqr/compositor"
	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/prompt"
	"xkiro-backend/internal/artqr/provider"
	"xkiro-backend/internal/artqr/qr"
	"xkiro-backend/internal/artqr/vision"
	"xkiro-backend/internal/qrtrans"
	"xkiro-backend/services"
)

const (
	jobsPersistenceFile    = "artqr_jobs.json"
	presetsPersistenceFile = "artqr_presets.json"
)

type Service struct {
	mu           sync.RWMutex
	jobs         map[string]*model.ArtQRJob
	presets      map[string]model.ArtQRPreset
	presetsOrder []string
	analyzer     vision.StyleAnalyzer
	provider     provider.ArtQRProvider
	machgen      *provider.MachGenProvider
	workerSem    chan struct{}
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

func (s *Service) savePresets() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]model.ArtQRPreset, 0)
	seen := make(map[string]bool)
	for _, id := range s.presetsOrder {
		if p, ok := s.presets[id]; ok && !seen[p.ID] {
			list = append(list, p)
			seen[p.ID] = true
		}
	}
	for id, p := range s.presets {
		if !seen[p.ID] && p.ID == id {
			list = append(list, p)
			seen[p.ID] = true
		}
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		_ = os.WriteFile(presetsPersistenceFile, data, 0644)
	}
}

func (s *Service) loadPresets() {
	data, err := os.ReadFile(presetsPersistenceFile)
	if err != nil {
		return
	}
	var loaded []model.ArtQRPreset
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pr := range loaded {
		s.presets[pr.ID] = pr
		if pr.Slug != "" {
			s.presets[pr.Slug] = pr
		}
		s.presetsOrder = append(s.presetsOrder, pr.ID)
	}
	log.Printf("[ArtQR] Restored %d presets from %s", len(loaded), presetsPersistenceFile)
}

func NewService() *Service {
	mg := provider.NewMachGenProvider()
	v := vision.NewXKiroVisionAnalyzer()

	presetMap := make(map[string]model.ArtQRPreset)
	order := make([]string, 0, len(prompt.DefaultPresets))
	for _, pr := range prompt.DefaultPresets {
		presetMap[pr.ID] = pr
		presetMap[pr.Slug] = pr
		order = append(order, pr.ID)
	}

	svc := &Service{
		jobs:         make(map[string]*model.ArtQRJob),
		presets:      presetMap,
		presetsOrder: order,
		analyzer:     v,
		provider:     mg,
		machgen:      mg,
		workerSem:    make(chan struct{}, 4),
	}
	svc.loadJobs()
	svc.loadPresets()
	return svc
}

func (s *Service) ListPresets() []model.ArtQRPreset {
	return s.GetPresets()
}

func (s *Service) GetPresets() []model.ArtQRPreset {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]model.ArtQRPreset, 0)
	seen := make(map[string]bool)

	for _, id := range s.presetsOrder {
		if p, ok := s.presets[id]; ok && !seen[p.ID] {
			result = append(result, p)
			seen[p.ID] = true
		}
	}
	for id, p := range s.presets {
		if !seen[p.ID] && p.ID == id {
			result = append(result, p)
			seen[p.ID] = true
		}
	}
	return result
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

func (s *Service) CreateOrUpdatePreset(p model.ArtQRPreset) error {
	s.mu.Lock()
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		p.ID = "preset_" + uuid.New().String()[:8]
	}
	if p.Slug == "" {
		p.Slug = p.ID
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	// Check if already in order
	alreadyInOrder := false
	for _, id := range s.presetsOrder {
		if id == p.ID {
			alreadyInOrder = true
			break
		}
	}
	if !alreadyInOrder {
		s.presetsOrder = append(s.presetsOrder, p.ID)
	}

	s.presets[p.ID] = p
	s.presets[p.Slug] = p
	s.mu.Unlock()

	s.savePresets()
	log.Printf("[ArtQR] Preset saved: ID=%s, Name=%q, Price=%d credits", p.ID, p.Name, p.PriceCredits)
	return nil
}

func (s *Service) DeletePreset(id string) error {
	s.mu.Lock()
	delete(s.presets, id)
	newOrder := make([]string, 0, len(s.presetsOrder))
	for _, pid := range s.presetsOrder {
		if pid != id {
			newOrder = append(newOrder, pid)
		}
	}
	s.presetsOrder = newOrder
	s.mu.Unlock()

	s.savePresets()
	log.Printf("[ArtQR] Preset deleted: ID=%s", id)
	return nil
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
	UserID         string          `json:"user_id,omitempty"`
	QRPNGBytes     []byte          `json:"-"`
	ReferenceBytes []byte          `json:"-"`
	PresetID       string          `json:"preset_id,omitempty"`
	CustomPrompt   string          `json:"custom_prompt,omitempty"`
	Placement      model.Placement `json:"placement"`
}

type ArtQRResult struct {
	Success           bool   `json:"success"`
	Image             string `json:"image"`
	ExpectedPayload   string `json:"expected_payload"`
	DecodedPayload    string `json:"decoded_payload"`
	QRValid           bool   `json:"qr_valid"`
	Preset            string `json:"preset"`
	BackgroundRemoved bool   `json:"background_removed"`
	FallbackMode      bool   `json:"fallback_mode"`
	RetryCount        int    `json:"retry_count"`
	ProcessingMs      int64  `json:"processing_ms"`
	Error             string `json:"error,omitempty"`
}

func (s *Service) AnalyzeStyle(ctx context.Context, refImgBytes []byte, placement model.Placement) (*vision.StyleAnalysisResult, error) {
	return s.analyzer.AnalyzeStyle(ctx, refImgBytes, placement)
}

// loadDefaultSceneImage loads prepackaged preset scene (e.g. Doraemon bread) if no reference was uploaded
func (s *Service) loadPresetSceneImage(preset *model.ArtQRPreset) []byte {
	if preset == nil {
		return loadDefaultSceneImage("bread_toast")
	}

	refURL := strings.TrimSpace(preset.ReferenceImageURL)
	if refURL == "" {
		refURL = strings.TrimSpace(preset.PreviewURL)
	}

	// 1. If it refers to /presets/<filename> or assets/<filename>
	if strings.HasPrefix(refURL, "/presets/") || strings.HasPrefix(refURL, "presets/") {
		filename := strings.TrimPrefix(refURL, "/presets/")
		filename = strings.TrimPrefix(filename, "presets/")
		candidates := []string{
			filepath.Join("assets", filename),
			filepath.Join("..", "..", "assets", filename),
			filepath.Join("../server/assets", filename),
			filepath.Join("client/public/presets", filename),
			filepath.Join("../client/public/presets", filename),
		}
		for _, c := range candidates {
			if data, err := os.ReadFile(c); err == nil && len(data) > 0 {
				return data
			}
		}
	}

	// 2. Default candidates for bread_toast
	if preset.ID == "bread_toast" || refURL == "" {
		return loadDefaultSceneImage("bread_toast")
	}

	// 3. Remote URL fallback
	if strings.HasPrefix(refURL, "http://") || strings.HasPrefix(refURL, "https://") {
		client := &http.Client{Timeout: 6 * time.Second}
		if resp, err := client.Get(refURL); err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)); err == nil && len(data) > 0 {
				return data
			}
		}
	}

	return loadDefaultSceneImage(preset.ID)
}

func loadDefaultSceneImage(presetID string) []byte {
	if presetID == "bread_toast" || presetID == "" {
		candidates := []string{
			"assets/doraemon_bread_scene.jpg",
			"../../assets/doraemon_bread_scene.jpg",
			"../server/assets/doraemon_bread_scene.jpg",
			"client/public/presets/doraemon_bread_scene.jpg",
			"../client/public/presets/doraemon_bread_scene.jpg",
		}
		for _, path := range candidates {
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				return data
			}
		}
	}
	return nil
}

// CreateJob validates original QR, runs existing QR background-removal immediately, and queues job
func (s *Service) CreateJob(ctx context.Context, params CreateJobParams) (*model.ArtQRJob, error) {
	jobID := "artqr_" + uuid.New().String()
	startTime := time.Now()

	log.Printf("[ArtQR] [%s] Pipeline started: validating raw QR input (%d bytes)", jobID, len(params.QRPNGBytes))

	if len(params.QRPNGBytes) == 0 {
		return nil, errors.New("qr_image is required")
	}

	// 1. STEP 2: Decode original QR BEFORE any generative processing. Reject if invalid.
	decoded, err := qr.DecodeQRCode(params.QRPNGBytes)
	if err != nil || decoded == nil || decoded.Payload == "" {
		log.Printf("[ArtQR] [%s] QR validation rejected: unable to decode original QR input (%v)", jobID, err)
		return nil, fmt.Errorf("không thể đọc được mã QR gốc; vui lòng tải ảnh QR rõ nét hơn: %v", err)
	}
	expectedPayload := decoded.Payload
	log.Printf("[ArtQR] [%s] Original QR successfully decoded. Expected payload: %q (hash=%s)",
		jobID, expectedPayload, decoded.PayloadHash)

	// 2. MANDATORY FIRST STEP: Run existing QR background removal immediately
	log.Printf("[ArtQR] [%s] MANDATORY STEP: Running existing QR background-removal (qrtrans.ProcessImage)...", jobID)

	rawImg, _, err := image.Decode(bytes.NewReader(params.QRPNGBytes))
	if err != nil {
		log.Printf("[ArtQR] [%s] Failed to decode raw QR image into image.Image: %v", jobID, err)
		return nil, fmt.Errorf("không thể đọc định dạng ảnh QR: %w", err)
	}

	var cleanedQRPNG []byte
	backgroundRemoved := false
	fallbackMode := false

	// Call existing QR background-removal function
	bgOpts := &qrtrans.Options{
		Threshold:                   0, // Automatic Otsu thresholding
		ValidateQR:                  true,
		FallbackOnValidationFailure: true,
		CropMode:                    qrtrans.CropModeCrop,
	}
	bgResult, bgErr := qrtrans.ProcessImage(rawImg, bgOpts)

	if bgErr != nil || bgResult == nil || len(bgResult.PNGData) == 0 {
		log.Printf("[ArtQR] [%s] Existing QR background-removal error: %v. Attempting safe fallback with original QR...", jobID, bgErr)
		// Safe fallback: only if original QR can still be decoded
		fallbackTest := qr.ValidateGeneratedQR(params.QRPNGBytes, expectedPayload)
		if fallbackTest.Valid {
			cleanedQRPNG = params.QRPNGBytes
			fallbackMode = true
			log.Printf("[ArtQR] [%s] Safe fallback mode engaged using verified original QR image", jobID)
		} else {
			log.Printf("[ArtQR] [%s] Failed safe fallback: original QR damaged or unrecoverable", jobID)
			return nil, fmt.Errorf("không thể bóc tách nền hoặc khôi phục mã QR an toàn: %v", bgErr)
		}
	} else {
		cleanedQRPNG = bgResult.PNGData
		backgroundRemoved = true
		log.Printf("[ArtQR] [%s] Existing QR background-removal SUCCEEDED in %v (threshold=%d, valid=%v, retries=%d)",
			jobID, bgResult.Duration, bgResult.ThresholdUsed, bgResult.QRValid, bgResult.Retries)
	}

	// 3. Resolve preset & placement
	if params.PresetID == "" {
		params.PresetID = "bread_toast"
	}
	preset, hasPreset := s.GetPreset(params.PresetID)
	if !hasPreset || preset == nil {
		preset = &prompt.DefaultPresets[0]
		params.PresetID = preset.ID
	}

	if !params.Placement.IsValid() {
		if preset.Placement != nil && preset.Placement.IsValid() {
			params.Placement = *preset.Placement
		} else {
			params.Placement = model.DefaultPlacement()
		}
	}

	// 4. Base scene reference image (Reference 1)
	baseScene := params.ReferenceBytes
	if len(baseScene) == 0 {
		baseScene = s.loadPresetSceneImage(preset)
		if len(baseScene) > 0 {
			log.Printf("[ArtQR] [%s] Loaded preset scene reference (%d bytes)", jobID, len(baseScene))
		}
	}
	if len(baseScene) == 0 {
		return nil, fmt.Errorf("không tải được ảnh tham chiếu của phong cách %q; không thể tạo Art QR chỉ có nền trống", preset.Name)
	}

	// 5. Build authoritative binary QR mask & control canvas
	quietZone := preset.QuietZoneModules
	if quietZone <= 0 {
		quietZone = 4
	}
	binaryMask, maskErr := qr.BuildBinaryQRMaskFromPayload(decoded.Payload, 1024, 1024, params.Placement, quietZone)
	if maskErr != nil {
		log.Printf("[ArtQR] [%s] Failed to build authoritative binary QR mask: %v", jobID, maskErr)
		return nil, fmt.Errorf("không thể khởi tạo mặt nạ module QR: %w", maskErr)
	}
	log.Printf("[ArtQR] [%s] Authoritative binary QR mask built: dimension=%dx%d modules, quiet_zone=%d",
		jobID, binaryMask.ModuleDim, binaryMask.ModuleDim, binaryMask.QuietZoneModules)

	controlCanvas, _ := qr.BuildControlCanvas(cleanedQRPNG, params.Placement, 1024)

	now := time.Now()
	job := &model.ArtQRJob{
		ID:                  jobID,
		UserID:              params.UserID,
		Status:              "queued",
		Progress:            10,
		OriginalPayload:     expectedPayload,
		OriginalPayloadHash: decoded.PayloadHash,
		PresetID:            params.PresetID,
		Prompt:              params.CustomPrompt,
		Placement:           params.Placement,
		MaxAttempts:         7,
		Attempts:            0,
		BackgroundRemoved:   backgroundRemoved,
		FallbackMode:        fallbackMode,
		ProcessingMs:        time.Since(startTime).Milliseconds(),
		SourceQRPNG:         params.QRPNGBytes,
		CleanedQRPNG:        cleanedQRPNG,
		ControlCanvasPNG:    controlCanvas,
		ReferenceImageJPEG:  baseScene,
		Images:              make([]model.OutputImage, 0),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()
	s.saveJobs()

	// Launch async execution worker
	go s.processJob(job, binaryMask, *preset)

	return job, nil
}

// GenerateArtQR executes the entire pipeline synchronously and returns the structured result contract
func (s *Service) GenerateArtQR(ctx context.Context, params CreateJobParams) (*ArtQRResult, error) {
	job, err := s.CreateJob(ctx, params)
	if err != nil {
		return &ArtQRResult{
			Success:           false,
			ExpectedPayload:   "",
			QRValid:           false,
			BackgroundRemoved: false,
			Error:             err.Error(),
		}, err
	}

	// Poll job until completion or failure (up to 2 minutes)
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(2 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return &ArtQRResult{
				Success:         false,
				ExpectedPayload: job.OriginalPayload,
				Error:           "thời gian xử lý vượt quá giới hạn",
			}, errors.New("timeout waiting for Art QR generation")
		case <-ticker.C:
			current, ok := s.GetJob(job.ID)
			if !ok {
				return nil, errors.New("tác vụ bị gián đoạn")
			}
			if current.Status == "completed" {
				var finalImageURL string
				var finalDecoded string
				if len(current.Images) > 0 {
					finalImageURL = current.Images[0].DataURL
					if finalImageURL == "" {
						finalImageURL = current.Images[0].URL
					}
					finalDecoded = current.Images[0].DecodedPayload
				}
				if finalDecoded == "" {
					finalDecoded = current.OriginalPayload
				}

				return &ArtQRResult{
					Success:           true,
					Image:             finalImageURL,
					ExpectedPayload:   current.OriginalPayload,
					DecodedPayload:    finalDecoded,
					QRValid:           true,
					Preset:            current.PresetID,
					BackgroundRemoved: current.BackgroundRemoved,
					FallbackMode:      current.FallbackMode,
					RetryCount:        current.Attempts - 1,
					ProcessingMs:      current.ProcessingMs,
				}, nil
			}
			if current.Status == "failed" {
				return &ArtQRResult{
					Success:           false,
					ExpectedPayload:   current.OriginalPayload,
					QRValid:           false,
					BackgroundRemoved: current.BackgroundRemoved,
					FallbackMode:      current.FallbackMode,
					RetryCount:        current.Attempts,
					ProcessingMs:      current.ProcessingMs,
					Error:             current.Error,
				}, fmt.Errorf("job failed: %s", current.Error)
			}
		}
	}
}

func (s *Service) processJob(job *model.ArtQRJob, binaryMask *qr.BinaryQRMask, preset model.ArtQRPreset) {
	s.workerSem <- struct{}{}
	defer func() { <-s.workerSem }()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	job.UpdateStatus("processing", 20)

	// Step A: Determine Prompt
	finalPrompt := preset.Prompt
	if strings.TrimSpace(job.Prompt) != "" {
		finalPrompt = strings.TrimSpace(job.Prompt)
	}
	job.Prompt = finalPrompt
	job.NegativePrompt = preset.NegativePrompt

	log.Printf("[ArtQR] [%s] Starting MachGen synthesis: preset=%s, placement=(%.2f, %.2f, %.2f)",
		job.ID, preset.ID, job.Placement.X, job.Placement.Y, job.Placement.Size)

	job.UpdateStatus("generating", 35)

	// Step B: MachGen Two-Reference Synthesis
	// Check if an active MachGen key was added by Admin in Rotator or configured in .env
	if services.DefaultRotator != nil {
		if mgKey, mgURL, mgModel := services.DefaultRotator.GetActiveMachGenKey(); mgKey != "" || mgURL != "" {
			s.machgen.Configure(mgURL, mgKey, mgModel)
		}
	}

	// Ref 1: Base scene image (ReferenceImageJPEG)
	// Ref 2: Cleaned transparent QR from background removal (CleanedQRPNG)
	machgenResultBytes, err := s.machgen.GenerateWithTwoReferences(
		ctx,
		job.ReferenceImageJPEG,
		job.CleanedQRPNG,
		finalPrompt,
		"", // Automatically uses active configured model (e.g. gpt-image-2, flux, etc.)
		1024,
		1024,
	)
	if err != nil {
		log.Printf("[ArtQR] [%s] MachGen dual-reference request failed (%v), proceeding to deterministic restoration with baseline scene", job.ID, err)
		// We still DO NOT fail immediately: the deterministic engine can blend the preset material directly onto the scene!
		machgenResultBytes = nil
	} else {
		log.Printf("[ArtQR] [%s] MachGen aesthetic generation received (%d bytes)", job.ID, len(machgenResultBytes))
	}

	job.UpdateStatus("validating", 65)

	// Step C: Progressive Safety Retry Pipeline (Attempts 1 to 7)
	safety := compositor.DefaultSafetyConfig(preset)
	var finalCompositedPNG []byte
	var decodedText string
	verified := false

	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		job.IncrementAttempt()

		// Progressive safety adjustments per Step 15
		switch attempt {
		case 1:
			// Baseline preset settings
			safety.MinLightLuminance = 185
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: baseline preset settings (texture=%.2f, contrast=%.2f, max_lum=%d, min_light=%d)",
				job.ID, attempt, safety.TextureStrength, safety.ContrastMultiplier, safety.MaxDarkLuminance, safety.MinLightLuminance)
		case 2:
			// Attempt 2: Reduce texture strength & lift light modules
			safety.TextureStrength = preset.TextureStrength * 0.65
			safety.MinLightLuminance = 195
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: reduced texture strength (texture=%.2f, min_light=%d)",
				job.ID, attempt, safety.TextureStrength, safety.MinLightLuminance)
		case 3:
			// Attempt 3: Increase dark/light contrast
			safety.ContrastMultiplier = preset.ContrastStrength * 1.20
			safety.MaxDarkLuminance = 85
			safety.MinLightLuminance = 205
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: increased contrast, clamped max_lum=%d, min_light=%d",
				job.ID, attempt, safety.MaxDarkLuminance, safety.MinLightLuminance)
		case 4:
			// Attempt 4: Make dark modules more uniformly dark toasted brown
			safety.TextureStrength = 0.06
			safety.MaxDarkLuminance = 70
			safety.MinLightLuminance = 215
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: uniform dark toasted brown modules, min_light=%d", job.ID, attempt, safety.MinLightLuminance)
		case 5:
			// Attempt 5: Reduce visual noise & clamp brighter pixels
			safety.TextureStrength = 0.03
			safety.MaxDarkLuminance = 55
			safety.MinLightLuminance = 220
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: minimal visual noise, aggressive dark clamping, min_light=%d", job.ID, attempt, safety.MinLightLuminance)
		case 6:
			// Attempt 6: Simplify finder-pattern material (100% solid contrast)
			safety.SolidifyFinderPattern = true
			safety.MaxDarkLuminance = 45
			safety.MinLightLuminance = 225
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: solidified finder patterns, min_light=%d", job.ID, attempt, safety.MinLightLuminance)
		case 7:
			// Attempt 7: Increase / clean quiet zone completely
			safety.CleanQuietZone = true
			safety.SolidifyFinderPattern = true
			safety.TextureStrength = 0.01
			safety.MaxDarkLuminance = 35
			safety.MinLightLuminance = 230
			log.Printf("[ArtQR] [%s] Restoration Attempt %d/7: enforced 100%% clean quiet zone & maximum contrast", job.ID, attempt)
		}

		// Execute Deterministic QR Restoration
		compBytes, compErr := compositor.RestoreAndComposite(
			job.ReferenceImageJPEG,
			machgenResultBytes,
			binaryMask,
			preset,
			safety,
		)
		if compErr != nil {
			log.Printf("[ArtQR] [%s] Attempt %d compositor error: %v", job.ID, attempt, compErr)
			continue
		}

		finalCompositedPNG = compBytes

		// Step D: QR Validation (Check decoded_payload == expected_payload)
		vResult := qr.ValidateGeneratedQRWithPlacement(compBytes, job.OriginalPayload, job.Placement)
		matchesPayload := (vResult.Valid && vResult.DecodedPayload == job.OriginalPayload)

		log.Printf("[ArtQR] [%s] Attempt %d validation: valid=%v, payloadMatch=%v (decoded=%q, expected=%q)",
			job.ID, attempt, vResult.Valid, matchesPayload, vResult.DecodedPayload, job.OriginalPayload)

		if matchesPayload {
			verified = true
			decodedText = vResult.DecodedPayload
			break
		}
	}

	// Safety Net: If standard attempts did not verify, run Guaranteed Fallback Restoration
	if !verified {
		log.Printf("[ArtQR] [%s] Standard attempts exhausted. Executing Guaranteed High-Contrast Safety Net...", job.ID)
		safetyNet := safety
		safetyNet.CleanQuietZone = true
		safetyNet.SolidifyFinderPattern = true
		safetyNet.TextureStrength = 0.0
		safetyNet.MaxDarkLuminance = 30
		safetyNet.MinLightLuminance = 235

		if compBytes, compErr := compositor.RestoreAndComposite(
			job.ReferenceImageJPEG,
			machgenResultBytes,
			binaryMask,
			preset,
			safetyNet,
		); compErr == nil {
			vResult := qr.ValidateGeneratedQRWithPlacement(compBytes, job.OriginalPayload, job.Placement)
			if vResult.Valid && vResult.DecodedPayload == job.OriginalPayload {
				verified = true
				decodedText = vResult.DecodedPayload
				finalCompositedPNG = compBytes
				log.Printf("[ArtQR] [%s] Guaranteed High-Contrast Safety Net SUCCEEDED!", job.ID)
			}
		}
	}

	job.ProcessingMs = time.Since(start).Milliseconds()

	// Step E: Final Output Contract
	if verified && len(finalCompositedPNG) > 0 {
		b64Data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(finalCompositedPNG)
		job.DecodedPayload = decodedText

		job.AddOutput(model.OutputImage{
			URL:                b64Data,
			DataURL:            b64Data,
			Verified:           true,
			DecodedPayload:     decodedText,
			DecodedPayloadHash: qr.HashPayload(decodedText),
			ConditioningScale:  preset.ConditioningScale,
		})

		job.UpdateStatus("completed", 100)
		log.Printf("[ArtQR] [%s] Job SUCCEEDED in %dms (attempts=%d, payloadMatch=true)",
			job.ID, job.ProcessingMs, job.Attempts)
	} else {
		job.SetError("Unable to generate a scanner-valid Art QR within retry budget")
		log.Printf("[ArtQR] [%s] Job FAILED: Unable to generate scanner-valid QR after %d retries",
			job.ID, job.Attempts)
	}

	s.saveJobs()
}
