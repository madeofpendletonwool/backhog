package metadata

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CoverCache stores cover art on local disk so the grid does not depend on the
// IGDB CDN at render time, and keeps working if IGDB is unreachable.
type CoverCache struct {
	dir  string
	http *http.Client
	// A page of search results fires a dozen cover requests at once. Letting
	// them all hit the CDN in parallel is what makes them time out; a small
	// gate keeps each individual download fast.
	slots chan struct{}
}

func NewCoverCache(dir string) (*CoverCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cover dir: %w", err)
	}
	return &CoverCache{
		dir:   dir,
		http:  &http.Client{Timeout: 30 * time.Second},
		slots: make(chan struct{}, 4),
	}, nil
}

// Path returns the on-disk location for a game's cover, whether or not it exists.
func (c *CoverCache) Path(gameID int64) string {
	return filepath.Join(c.dir, fmt.Sprintf("%d.jpg", gameID))
}

// Has reports whether a cover has already been downloaded.
func (c *CoverCache) Has(gameID int64) bool {
	info, err := os.Stat(c.Path(gameID))
	return err == nil && info.Size() > 0
}

// Fetch downloads a cover if it is not already cached and returns an accent
// colour sampled from the artwork. Writes go to a temp file first so a failed
// download can never leave a truncated image in the cache.
func (c *CoverCache) Fetch(ctx context.Context, gameID int64, url string) (accent string, err error) {
	if url == "" {
		return "", nil
	}
	dest := c.Path(gameID)
	if c.Has(gameID) {
		return accentFromFile(dest), nil
	}

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Another request may have won the race for this cover while we queued.
	if c.Has(gameID) {
		return accentFromFile(dest), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("download cover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download cover: status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(c.dir, "cover-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 10<<20)); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write cover: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", fmt.Errorf("commit cover: %w", err)
	}

	return accentFromFile(dest), nil
}

// accentFromFile samples a representative colour for UI tinting. Failure is not
// interesting — the UI falls back to a neutral accent — so errors become "".
func accentFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return ""
	}
	return dominantColor(img)
}

// dominantColor averages the image on a coarse grid, weighting saturated pixels
// far more heavily than grey ones. A plain mean tends toward muddy brown; this
// keeps the accent recognisably related to the cover art.
func dominantColor(img image.Image) string {
	bounds := img.Bounds()
	if bounds.Empty() {
		return ""
	}

	const samples = 32
	stepX := max(bounds.Dx()/samples, 1)
	stepY := max(bounds.Dy()/samples, 1)

	var sumR, sumG, sumB, sumW float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 == 0 {
				continue
			}
			r := float64(r16 >> 8)
			g := float64(g16 >> 8)
			b := float64(b16 >> 8)

			maxC := math.Max(r, math.Max(g, b))
			minC := math.Min(r, math.Min(g, b))
			if maxC == 0 {
				continue
			}
			saturation := (maxC - minC) / maxC
			// Discount near-black and near-white pixels, which carry no hue.
			luma := (0.299*r + 0.587*g + 0.114*b) / 255
			brightness := 1 - math.Abs(luma-0.55)/0.55

			weight := saturation*saturation*math.Max(brightness, 0) + 0.02
			sumR += r * weight
			sumG += g * weight
			sumB += b * weight
			sumW += weight
		}
	}
	if sumW == 0 {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x",
		clamp8(sumR/sumW), clamp8(sumG/sumW), clamp8(sumB/sumW))
}

func clamp8(v float64) int {
	return min(max(int(v+0.5), 0), 255)
}
