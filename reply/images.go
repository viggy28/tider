package reply

import (
	"net/url"
	"strings"
)

// MaxImagesForAnalysis caps how many product/page images we attach to
// the visual analyzer prompt. The screenshot is always attached
// separately; this is the cap on the additional images. 8 keeps cost
// bounded while leaving room for the most informative product shots
// most ecommerce homepages surface above the fold.
const MaxImagesForAnalysis = 8

// imageNoiseFragments are URL substrings that mark an image as low-
// signal for visual review: logos, icons, payment badges, sprites,
// social glyphs, tracking pixels. Filtered case-insensitively.
//
// Conservative on purpose — false negatives cost a few extra tokens at
// the visual analyzer; false positives drop a real product image.
var imageNoiseFragments = []string{
	"logo",
	"favicon",
	"sprite",
	"icon-",
	"/icons/",
	"payment",
	"badge",
	"tracking",
	"pixel",
	"google-analytics",
	"facebook.com/tr",
}

// SelectImagesForAnalysis filters Firecrawl's extracted image URL list
// to the set of likely product/page images worth sending to the visual
// analyzer. Returns up to MaxImagesForAnalysis URLs, deduped
// case-insensitively, body-order preserved (Firecrawl emits images in
// roughly DOM order, which correlates with above-the-fold).
//
// Filters:
//   - reject URLs containing any imageNoiseFragments substring
//   - reject non-http(s) schemes (data: URIs, relative paths)
//   - reject the screenshot URL itself if it appears in the list
//   - dedupe case-insensitively
func SelectImagesForAnalysis(rawURLs []string, screenshotURL string) []string {
	if len(rawURLs) == 0 {
		return nil
	}
	screenshotLower := strings.ToLower(strings.TrimSpace(screenshotURL))
	seen := map[string]bool{}
	var out []string
	for _, raw := range rawURLs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		lower := strings.ToLower(raw)
		if lower == screenshotLower {
			continue
		}
		if isLowSignalImageURL(lower) {
			continue
		}
		if seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, raw)
		if len(out) >= MaxImagesForAnalysis {
			break
		}
	}
	return out
}

func isLowSignalImageURL(lowerURL string) bool {
	for _, frag := range imageNoiseFragments {
		if strings.Contains(lowerURL, frag) {
			return true
		}
	}
	return false
}
