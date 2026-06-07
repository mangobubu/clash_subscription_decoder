package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand"
	"strconv"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/mojocn/base64Captcha"
	"golang.org/x/image/font"
)

const (
	defaultCaptchaLength     = 5
	defaultCaptchaWidth      = 160
	defaultCaptchaHeight     = 60
	defaultCaptchaBgColor    = "rgba(15, 23, 42, 0.6)"
	defaultCaptchaTextColor  = "#FFFFFF"
	defaultCaptchaSource     = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"
	defaultCaptchaNoiseCount = 18
	defaultCaptchaNoiseColor = "rgba(148, 163, 184, 0.45)"

	captchaMinLength = 1
	captchaMaxLength = 12
	captchaMinSize   = 24
)

var captchaFontNames = []string{"fonts/wqy-microhei.ttc"}

type stringCaptchaDriver struct {
	height     int
	width      int
	length     int
	source     string
	noiseCount int
	bgColor    color.RGBA
	textColor  color.RGBA
	noiseColor color.RGBA
	fonts      []*truetype.Font
}

type stringCaptchaItem struct {
	img *image.NRGBA
}

func newConfiguredCaptchaDriver() *stringCaptchaDriver {
	return &stringCaptchaDriver{
		height:     normalizeCaptchaSize(AppConfig.Auth.CaptchaHeight, defaultCaptchaHeight),
		width:      normalizeCaptchaSize(AppConfig.Auth.CaptchaWidth, defaultCaptchaWidth),
		length:     normalizeCaptchaLength(AppConfig.Auth.CaptchaLength),
		source:     normalizeCaptchaSource(AppConfig.Auth.CaptchaSource),
		noiseCount: normalizeCaptchaNoiseCount(AppConfig.Auth.CaptchaNoiseCount),
		bgColor:    parseColorOrDefault(AppConfig.Auth.CaptchaBgColor, defaultCaptchaBgColor),
		textColor:  parseColorOrDefault(AppConfig.Auth.CaptchaTextColor, defaultCaptchaTextColor),
		noiseColor: parseColorOrDefault(AppConfig.Auth.CaptchaNoiseColor, defaultCaptchaNoiseColor),
		fonts:      loadCaptchaFonts(),
	}
}

func (d *stringCaptchaDriver) GenerateIdQuestionAnswer() (id, content, answer string) {
	answer = base64Captcha.RandText(d.length, d.source)
	return base64Captcha.RandomId(), answer, answer
}

func (d *stringCaptchaDriver) DrawCaptcha(content string) (base64Captcha.Item, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("captcha content must not be empty")
	}

	item := newStringCaptchaItem(d.width, d.height, d.bgColor)
	item.drawNoiseDots(d.noiseCount, d.noiseColor)
	if err := item.drawText(content, d.textColor, d.fonts); err != nil {
		return nil, err
	}
	return item, nil
}

func newStringCaptchaItem(width int, height int, bgColor color.RGBA) *stringCaptchaItem {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bgColor}, image.Point{}, draw.Src)
	return &stringCaptchaItem{img: img}
}

func (item *stringCaptchaItem) drawNoiseDots(count int, dotColor color.RGBA) {
	if count <= 0 {
		return
	}

	bounds := item.img.Bounds()
	for i := 0; i < count; i++ {
		x := randomInt(bounds.Dx())
		y := randomInt(bounds.Dy())
		radius := 1 + randomInt(2)
		item.drawDot(x, y, radius, dotColor)
	}
}

func (item *stringCaptchaItem) drawDot(centerX int, centerY int, radius int, dotColor color.RGBA) {
	for y := centerY - radius; y <= centerY+radius; y++ {
		for x := centerX - radius; x <= centerX+radius; x++ {
			if x < 0 || y < 0 || x >= item.img.Bounds().Dx() || y >= item.img.Bounds().Dy() {
				continue
			}
			if math.Hypot(float64(x-centerX), float64(y-centerY)) <= float64(radius) {
				item.img.SetNRGBA(x, y, color.NRGBA{
					R: dotColor.R,
					G: dotColor.G,
					B: dotColor.B,
					A: dotColor.A,
				})
			}
		}
	}
}

func (item *stringCaptchaItem) drawText(text string, textColor color.RGBA, fonts []*truetype.Font) error {
	runes := []rune(text)
	if len(runes) == 0 {
		return errors.New("captcha text must not be empty")
	}
	if len(fonts) == 0 {
		return errors.New("captcha font is unavailable")
	}

	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetClip(item.img.Bounds())
	c.SetDst(item.img)
	c.SetHinting(font.HintingFull)
	c.SetSrc(image.NewUniform(textColor))

	slotWidth := item.img.Bounds().Dx() / len(runes)
	fontSize := calculateCaptchaFontSize(item.img.Bounds().Dy(), slotWidth)
	c.SetFontSize(float64(fontSize))
	c.SetFont(fonts[0])

	for i, r := range runes {
		x := slotWidth*i + maxInt(2, slotWidth/5)
		yBase := item.img.Bounds().Dy()/2 + fontSize/3
		y := yBase + randomInt(maxInt(1, item.img.Bounds().Dy()/12)) - item.img.Bounds().Dy()/24
		if _, err := c.DrawString(string(r), freetype.Pt(x, y)); err != nil {
			return err
		}
	}
	return nil
}

func (item *stringCaptchaItem) WriteTo(w io.Writer) (int64, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, item.img); err != nil {
		return 0, err
	}
	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

func (item *stringCaptchaItem) EncodeB64string() string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, item.img); err != nil {
		return ""
	}
	return fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(buf.Bytes()))
}

func loadCaptchaFonts() []*truetype.Font {
	fonts := base64Captcha.DefaultEmbeddedFonts.LoadFontsByNames(captchaFontNames)
	if len(fonts) > 0 {
		return fonts
	}
	if font := base64Captcha.DefaultEmbeddedFonts.LoadFontByName("fonts/wqy-microhei.ttc"); font != nil {
		return []*truetype.Font{font}
	}
	return nil
}

func normalizeCaptchaLength(length int) int {
	if length < captchaMinLength {
		return defaultCaptchaLength
	}
	if length > captchaMaxLength {
		return captchaMaxLength
	}
	return length
}

func normalizeCaptchaSize(size int, fallback int) int {
	if size < captchaMinSize {
		return fallback
	}
	return size
}

func normalizeCaptchaSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return defaultCaptchaSource
	}
	return source
}

func normalizeCaptchaNoiseCount(count int) int {
	if count < 0 {
		return 0
	}
	if count == 0 {
		return 0
	}
	return count
}

func parseColorOrDefault(value string, fallback string) color.RGBA {
	if parsed, ok := parseCaptchaColor(value); ok {
		return parsed
	}
	if parsed, ok := parseCaptchaColor(fallback); ok {
		return parsed
	}
	return color.RGBA{}
}

func parseCaptchaColor(value string) (color.RGBA, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return color.RGBA{}, false
	}

	if strings.HasPrefix(value, "#") {
		hexValue := strings.TrimPrefix(value, "#")
		if len(hexValue) != 6 && len(hexValue) != 8 {
			return color.RGBA{}, false
		}
		rgba, err := strconv.ParseUint(hexValue, 16, 32)
		if err != nil {
			return color.RGBA{}, false
		}
		if len(hexValue) == 6 {
			return color.RGBA{
				R: uint8(rgba >> 16),
				G: uint8(rgba >> 8),
				B: uint8(rgba),
				A: 255,
			}, true
		}
		return color.RGBA{
			R: uint8(rgba >> 24),
			G: uint8(rgba >> 16),
			B: uint8(rgba >> 8),
			A: uint8(rgba),
		}, true
	}

	normalized := strings.ReplaceAll(value, " ", "")
	if !strings.HasPrefix(normalized, "rgba(") || !strings.HasSuffix(normalized, ")") {
		return color.RGBA{}, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(normalized, "rgba("), ")"), ",")
	if len(parts) != 4 {
		return color.RGBA{}, false
	}

	r, errR := strconv.Atoi(parts[0])
	g, errG := strconv.Atoi(parts[1])
	b, errB := strconv.Atoi(parts[2])
	a, errA := strconv.ParseFloat(parts[3], 64)
	if errR != nil || errG != nil || errB != nil || errA != nil {
		return color.RGBA{}, false
	}
	if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 || a < 0 || a > 1 {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a * 255),
	}, true
}

func calculateCaptchaFontSize(height int, slotWidth int) int {
	byHeight := int(float64(height) * 0.58)
	byWidth := int(float64(slotWidth) * 0.86)
	size := minInt(byHeight, byWidth)
	return maxInt(16, size)
}

func randomInt(max int) int {
	if max <= 0 {
		return 0
	}
	return rand.Intn(max)
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
