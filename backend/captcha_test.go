package main

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestStringCaptchaDriverGeneratesAnswerFromSource(t *testing.T) {
	driver := &stringCaptchaDriver{
		length: 8,
		source: "AB",
	}

	_, question, answer := driver.GenerateIdQuestionAnswer()
	if question != answer {
		t.Fatalf("question and answer should match, got %q and %q", question, answer)
	}
	if len([]rune(answer)) != driver.length {
		t.Fatalf("answer length = %d, want %d", len([]rune(answer)), driver.length)
	}
	for _, r := range answer {
		if !strings.ContainsRune(driver.source, r) {
			t.Fatalf("answer contains rune %q outside source %q", r, driver.source)
		}
	}
}

func TestStringCaptchaDriverDrawsPNG(t *testing.T) {
	driver := &stringCaptchaDriver{
		height:     60,
		width:      160,
		length:     5,
		source:     "ABCDEF",
		noiseCount: 3,
		bgColor:    color.RGBA{R: 15, G: 23, B: 42, A: 255},
		textColor:  color.RGBA{R: 255, G: 255, B: 255, A: 255},
		noiseColor: color.RGBA{R: 148, G: 163, B: 184, A: 255},
		fonts:      loadCaptchaFonts(),
	}

	item, err := driver.DrawCaptcha("ABCDE")
	if err != nil {
		t.Fatalf("DrawCaptcha failed: %v", err)
	}

	const prefix = "data:image/png;base64,"
	encoded := item.EncodeB64string()
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatalf("captcha image prefix = %q", encoded[:minInt(len(encoded), len(prefix))])
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		t.Fatalf("decode captcha image failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png decode failed: %v", err)
	}
	if img.Bounds().Dx() != driver.width || img.Bounds().Dy() != driver.height {
		t.Fatalf("image size = %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), driver.width, driver.height)
	}
}

func TestParseColorOrDefault(t *testing.T) {
	if got := parseColorOrDefault("#112233", defaultCaptchaTextColor); got != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 255}) {
		t.Fatalf("hex color = %#v", got)
	}
	if got := parseColorOrDefault("rgba(1, 2, 3, 0.5)", defaultCaptchaTextColor); got != (color.RGBA{R: 1, G: 2, B: 3, A: 127}) {
		t.Fatalf("rgba color = %#v", got)
	}
	if got := parseColorOrDefault("not-a-color", "#445566"); got != (color.RGBA{R: 0x44, G: 0x55, B: 0x66, A: 255}) {
		t.Fatalf("fallback color = %#v", got)
	}
	if got := parseColorOrDefault("#11223300", defaultCaptchaTextColor); got != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0}) {
		t.Fatalf("transparent color = %#v", got)
	}
}

func TestApplyCaptchaConfigDefaults(t *testing.T) {
	var cfg Config
	applyCaptchaConfigDefaults(&cfg, []byte("[auth]\ncaptcha_noise_count = 0\n"))

	if cfg.Auth.CaptchaLength != defaultCaptchaLength {
		t.Fatalf("CaptchaLength = %d, want %d", cfg.Auth.CaptchaLength, defaultCaptchaLength)
	}
	if cfg.Auth.CaptchaSource != defaultCaptchaSource {
		t.Fatalf("CaptchaSource = %q, want default source", cfg.Auth.CaptchaSource)
	}
	if cfg.Auth.CaptchaNoiseCount != 0 {
		t.Fatalf("CaptchaNoiseCount = %d, want explicit zero", cfg.Auth.CaptchaNoiseCount)
	}

	var cfgWithoutNoise Config
	applyCaptchaConfigDefaults(&cfgWithoutNoise, []byte("[auth]\n"))
	if cfgWithoutNoise.Auth.CaptchaNoiseCount != defaultCaptchaNoiseCount {
		t.Fatalf("CaptchaNoiseCount = %d, want default %d", cfgWithoutNoise.Auth.CaptchaNoiseCount, defaultCaptchaNoiseCount)
	}
}
