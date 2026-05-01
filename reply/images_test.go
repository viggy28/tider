package reply

import (
	"strings"
	"testing"
)

func TestSelectImagesForAnalysisFiltersNoise(t *testing.T) {
	raw := []string{
		"https://example.com/logo.png",                  // reject: logo
		"https://example.com/products/bowl-1.jpg",       // keep: product
		"https://example.com/icons/payment-visa.svg",    // reject: payment + icons + svg-as-icon shape
		"https://example.com/products/bowl-2.jpg",       // keep
		"https://example.com/img/favicon.ico",           // reject: favicon
		"https://example.com/products/bowl-1.JPG",       // dedupe (case-insensitive)
		"https://www.facebook.com/tr?id=123",            // reject: tracking pixel
		"data:image/png;base64,iVBOR...",                // reject: data URI (no scheme http(s))
		"/relative/path.jpg",                            // reject: relative
		"https://example.com/products/bowl-3.webp",      // keep
		"https://example.com/icon-search.svg",           // reject: icon-
		"https://example.com/sprite-nav.png",            // reject: sprite
	}
	got := SelectImagesForAnalysis(raw, "")
	want := []string{
		"https://example.com/products/bowl-1.jpg",
		"https://example.com/products/bowl-2.jpg",
		"https://example.com/products/bowl-3.webp",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d images, want %d: got=%v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSelectImagesForAnalysisCaps(t *testing.T) {
	var raw []string
	for i := 0; i < 50; i++ {
		raw = append(raw, "https://example.com/products/p"+itoa(i)+".jpg")
	}
	got := SelectImagesForAnalysis(raw, "")
	if len(got) != MaxImagesForAnalysis {
		t.Errorf("expected cap at %d, got %d", MaxImagesForAnalysis, len(got))
	}
	// Body order preserved — first 8 should be p0..p7.
	for i, u := range got {
		if !strings.Contains(u, "/p"+itoa(i)+".jpg") {
			t.Errorf("got[%d] = %q, expected to contain p%d", i, u, i)
		}
	}
}

func TestSelectImagesForAnalysisExcludesScreenshotURL(t *testing.T) {
	screenshot := "https://firecrawl-cdn.example/screenshots/abc123.png"
	raw := []string{
		"https://example.com/products/bowl.jpg",
		screenshot,                            // should be excluded
		strings.ToUpper(screenshot),           // case-insensitive dedupe
		"https://example.com/products/mug.jpg",
	}
	got := SelectImagesForAnalysis(raw, screenshot)
	for _, u := range got {
		if strings.EqualFold(u, screenshot) {
			t.Errorf("screenshot URL %q should be excluded from product images", u)
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 product images after screenshot filter, got %d: %v", len(got), got)
	}
}

func TestSelectImagesForAnalysisEmptyInput(t *testing.T) {
	if got := SelectImagesForAnalysis(nil, ""); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
	if got := SelectImagesForAnalysis([]string{}, ""); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

// itoa: tiny stdlib-free int-to-string for tests, avoids strconv import
// for a single use case.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
