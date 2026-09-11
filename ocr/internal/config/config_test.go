package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("OCR_WORKER_TOKEN", "secret")
	t.Setenv("OCR_WORKER_ID", "w1")
	for _, key := range []string{"OCR_TESSERACT_PSM", "OCR_PAGE_BATCH", "OCR_PAGE_TIMEOUT",
		"OCR_POLL_INTERVAL", "OCR_HEARTBEAT_INTERVAL", "OCR_LANGUAGE", "TESSERACT_BIN",
		"OCR_WORK_DIR", "OCR_STATUS_ADDR", "BACKHOG_API_URL"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PSM != 11 {
		t.Errorf("psm = %d, want 11 (sparse text — the lettering case)", cfg.PSM)
	}
	if cfg.Language != "eng" || cfg.TesseractBin != "tesseract" {
		t.Errorf("bin/language = %s/%s", cfg.TesseractBin, cfg.Language)
	}
	if cfg.PageBatch <= 0 || cfg.PageTimeout <= 0 {
		t.Errorf("batch/timeout = %d/%s", cfg.PageBatch, cfg.PageTimeout)
	}
}

func TestLoadRequiresToken(t *testing.T) {
	t.Setenv("OCR_WORKER_TOKEN", "")
	cfg, err := Load()
	if err == nil {
		t.Fatal("empty token accepted")
	}
	_ = cfg
}

func TestLoadRejectsBadTuning(t *testing.T) {
	t.Setenv("OCR_WORKER_TOKEN", "secret")
	t.Setenv("OCR_TESSERACT_PSM", "99")
	if _, err := Load(); err == nil {
		t.Fatal("psm 99 accepted")
	}
	t.Setenv("OCR_TESSERACT_PSM", "3")
	t.Setenv("OCR_PAGE_TIMEOUT", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("zero timeout accepted")
	}
}
