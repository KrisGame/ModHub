package catalog

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

type cachedIconBlob struct {
	data []byte
	mime string
	etag string
}

type iconDiskWrite struct {
	cacheFile string
	metaFile  string
	data      []byte
	mime      string
}

type IconCache struct {
	mediaDir   string
	fetchSem   chan struct{}
	memCache   *storage.LRUCache[cachedIconBlob]
	sf         *storage.SingleFlight[cachedIconBlob]
	pngEnc     *png.Encoder
	diskCh     chan iconDiskWrite
	stopCh     chan struct{}
	diskDone   chan struct{}
	closeOnce  sync.Once
	dirOnce    sync.Once
	transport  *http.Transport
	httpClient *http.Client
}

func NewIconCache(mediaDir string) *IconCache {
	tr := &http.Transport{
		Proxy: netx.ResolveProxyFunc,
		DialContext: netx.MakeSafeDialContext(&net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
		MaxIdleConns:        24,
		MaxIdleConnsPerHost: 12,
		IdleConnTimeout:     45 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	ic := &IconCache{
		mediaDir:  mediaDir,
		fetchSem:  make(chan struct{}, 8),
		memCache:  storage.NewLRUCache[cachedIconBlob](512),
		sf:        storage.NewSingleFlight[cachedIconBlob](),
		pngEnc:    &png.Encoder{CompressionLevel: png.BestCompression},
		diskCh:    make(chan iconDiskWrite, 128),
		stopCh:    make(chan struct{}),
		diskDone:  make(chan struct{}),
		transport: tr,
		httpClient: &http.Client{
			Timeout:       10 * time.Second,
			Transport:     tr,
			CheckRedirect: netx.MakeSafeRedirectChecker(netx.ValidateIconURL),
		},
	}
	go ic.runDiskWriter()
	return ic
}

func (ic *IconCache) runDiskWriter() {
	defer close(ic.diskDone)
	writeJob := func(job iconDiskWrite) {
		tmpFile := job.cacheFile + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 36)
		if os.WriteFile(tmpFile, job.data, 0o644) == nil {
			_ = os.Rename(tmpFile, job.cacheFile)
		}
		_ = os.WriteFile(job.metaFile, []byte(job.mime), 0o644)
	}
	for {
		select {
		case job := <-ic.diskCh:
			writeJob(job)
		case <-ic.stopCh:
			for {
				select {
				case job := <-ic.diskCh:
					writeJob(job)
				default:
					return
				}
			}
		}
	}
}

func (ic *IconCache) Close() {
	if ic == nil {
		return
	}
	ic.closeOnce.Do(func() {
		close(ic.stopCh)
		<-ic.diskDone
		ic.ResetConnections()
	})
}

func (ic *IconCache) Clear() {
	if ic != nil && ic.memCache != nil {
		ic.memCache.Clear()
	}
}

func (ic *IconCache) ResetConnections() {
	if ic != nil && ic.transport != nil {
		ic.transport.CloseIdleConnections()
	}
}

func (ic *IconCache) ensureMediaDir() {
	ic.dirOnce.Do(func() { _ = os.MkdirAll(ic.mediaDir, 0o755) })
}

func IsSafeRasterImageMIME(mimeStr string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(mimeStr, ";")[0]))
	switch base {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/x-icon", "image/vnd.microsoft.icon":
		return true
	default:
		return false
	}
}

func DetectSafeIconMIME(data []byte) string {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" && (strings.Contains(string(data[8:12]), "avif") || strings.Contains(string(data[8:12]), "avis")) {
		return "image/avif"
	}
	detected := http.DetectContentType(data)
	if IsSafeRasterImageMIME(detected) {
		return strings.Split(detected, ";")[0]
	}
	return ""
}

func trimTransparentMargins(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 24 || h < 24 {
		return img
	}
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0x1800 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < minX || maxY < minY {
		return img
	}
	cw := maxX - minX + 1
	ch := maxY - minY + 1
	if cw < 16 || ch < 16 {
		return img
	}
	// Only crop when significant transparent letterboxing/margin exists (>= 14% along either axis)
	if (w-cw)*100/w < 14 && (h-ch)*100/h < 14 {
		return img
	}
	dst := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			dst.Set(x, y, img.At(minX+x, minY+y))
		}
	}
	return dst
}

func downscaleImageSquare(src image.Image, maxDim int) *image.NRGBA {
	src = trimTransparentMargins(src)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if w > maxDim || h > maxDim {
		if w >= h {
			nw = maxDim
			nh = (h * maxDim) / w
		} else {
			nh = maxDim
			nw = (w * maxDim) / h
		}
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	dpix := dst.Pix
	dstride := dst.Stride

	switch s := src.(type) {
	case *image.NRGBA:
		for y := 0; y < nh; y++ {
			sy := b.Min.Y + (y*h)/nh
			srow := (sy - s.Rect.Min.Y) * s.Stride
			drow := y * dstride
			for x := 0; x < nw; x++ {
				sx := b.Min.X + (x*w)/nw
				si := srow + (sx-s.Rect.Min.X)*4
				di := drow + x*4
				dpix[di] = s.Pix[si]
				dpix[di+1] = s.Pix[si+1]
				dpix[di+2] = s.Pix[si+2]
				dpix[di+3] = s.Pix[si+3]
			}
		}
	case *image.RGBA:
		for y := 0; y < nh; y++ {
			sy := b.Min.Y + (y*h)/nh
			srow := (sy - s.Rect.Min.Y) * s.Stride
			drow := y * dstride
			for x := 0; x < nw; x++ {
				sx := b.Min.X + (x*w)/nw
				si := srow + (sx-s.Rect.Min.X)*4
				di := drow + x*4
				a := s.Pix[si+3]
				if a == 0xff {
					dpix[di] = s.Pix[si]
					dpix[di+1] = s.Pix[si+1]
					dpix[di+2] = s.Pix[si+2]
					dpix[di+3] = 0xff
				} else if a == 0 {
					dpix[di], dpix[di+1], dpix[di+2], dpix[di+3] = 0, 0, 0, 0
				} else {
					a32 := uint32(a)
					dpix[di] = uint8((uint32(s.Pix[si]) * 0xff) / a32)
					dpix[di+1] = uint8((uint32(s.Pix[si+1]) * 0xff) / a32)
					dpix[di+2] = uint8((uint32(s.Pix[si+2]) * 0xff) / a32)
					dpix[di+3] = a
				}
			}
		}
	case *image.YCbCr:
		for y := 0; y < nh; y++ {
			sy := b.Min.Y + (y*h)/nh
			drow := y * dstride
			for x := 0; x < nw; x++ {
				sx := b.Min.X + (x*w)/nw
				yi := s.YOffset(sx, sy)
				ci := s.COffset(sx, sy)
				r, g, bl := color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
				di := drow + x*4
				dpix[di] = r
				dpix[di+1] = g
				dpix[di+2] = bl
				dpix[di+3] = 0xff
			}
		}
	default:
		for y := 0; y < nh; y++ {
			sy := b.Min.Y + (y*h)/nh
			drow := y * dstride
			for x := 0; x < nw; x++ {
				sx := b.Min.X + (x*w)/nw
				r, g, bl, a := src.At(sx, sy).RGBA()
				di := drow + x*4
				if a == 0xffff {
					dpix[di] = uint8(r >> 8)
					dpix[di+1] = uint8(g >> 8)
					dpix[di+2] = uint8(bl >> 8)
					dpix[di+3] = 0xff
				} else if a == 0 {
					dpix[di], dpix[di+1], dpix[di+2], dpix[di+3] = 0, 0, 0, 0
				} else {
					dpix[di] = uint8(((r * 0xffff) / a) >> 8)
					dpix[di+1] = uint8(((g * 0xffff) / a) >> 8)
					dpix[di+2] = uint8(((bl * 0xffff) / a) >> 8)
					dpix[di+3] = uint8(a >> 8)
				}
			}
		}
	}
	return dst
}

func isNRGBAOpaque(img *image.NRGBA) bool {
	pix := img.Pix
	for i := 3; i < len(pix); i += 4 {
		if pix[i] < 248 {
			return false
		}
	}
	return true
}


func (ic *IconCache) GetCachedIcon(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	if _, err := netx.ValidateIconURL(rawURL); err != nil {
		return nil, "", "", err
	}
	h := sha1.Sum([]byte("v4|" + rawURL))
	hexKey := hex.EncodeToString(h[:])[:20]

	if blob, ok := ic.memCache.Get(hexKey); ok {
		return blob.data, blob.mime, blob.etag, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	blob, err := ic.sf.DoCtx(ctx, hexKey, func() (cachedIconBlob, error) {
		fetchCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		if b, ok := ic.memCache.Get(hexKey); ok {
			return b, nil
		}
		etag := `"` + hexKey + `"`
		ic.ensureMediaDir()
		cacheFile := filepath.Join(ic.mediaDir, hexKey+".bin")
		metaFile := filepath.Join(ic.mediaDir, hexKey+".mime")

		if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
			mimeBytes, _ := os.ReadFile(metaFile)
			mimeStr := strings.TrimSpace(string(mimeBytes))
			if !IsSafeRasterImageMIME(mimeStr) {
				mimeStr = DetectSafeIconMIME(data)
			}
			if IsSafeRasterImageMIME(mimeStr) {
				b := cachedIconBlob{data: data, mime: mimeStr, etag: etag}
				ic.memCache.Set(hexKey, b, 3600)
				return b, nil
			}
		}

		if !netx.AcquireSemCtx(fetchCtx, ic.fetchSem) {
			return cachedIconBlob{}, fetchCtx.Err()
		}
		defer func() { <-ic.fetchSem }()

		req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			return cachedIconBlob{}, err
		}
		req.Header.Set("User-Agent", domain.UserAgent)
		resp, err := ic.httpClient.Do(req)
		if err != nil {
			return cachedIconBlob{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return cachedIconBlob{}, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		const maxBytes = 6 * 1024 * 1024
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return cachedIconBlob{}, err
		}
		if len(data) > maxBytes {
			return cachedIconBlob{}, fmt.Errorf("icon too large")
		}

		mimeStr := DetectSafeIconMIME(data)
		if mimeStr == "" {
			return cachedIconBlob{}, fmt.Errorf("неподдерживаемый формат иконки (%s)", http.DetectContentType(data))
		}
		if len(data) > 20*1024 || strings.HasPrefix(mimeStr, "image/gif") || strings.Contains(mimeStr, "png") || strings.Contains(mimeStr, "webp") {
			if img, _, decErr := image.Decode(bytes.NewReader(data)); decErr == nil {
				small := downscaleImageSquare(img, 144)
				var outBuf bytes.Buffer
				if isNRGBAOpaque(small) {
					if jpeg.Encode(&outBuf, small, &jpeg.Options{Quality: 86}) == nil && outBuf.Len() > 0 {
						data = outBuf.Bytes()
						mimeStr = "image/jpeg"
					}
				} else {
					if ic.pngEnc.Encode(&outBuf, small) == nil && outBuf.Len() > 0 {
						data = outBuf.Bytes()
						mimeStr = "image/png"
					}
				}
			}
		}

		b := cachedIconBlob{data: data, mime: mimeStr, etag: etag}
		ic.memCache.Set(hexKey, b, 3600)

		select {
		case ic.diskCh <- iconDiskWrite{cacheFile: cacheFile, metaFile: metaFile, data: data, mime: mimeStr}:
		default:
		}

		return b, nil
	})
	if err != nil {
		return nil, "", "", err
	}
	return blob.data, blob.mime, blob.etag, nil
}

func (ic *IconCache) Stats() map[string]any {
	return GetMediaCacheStats(ic.mediaDir)
}

func GetMediaCacheStats(mediaDir string) map[string]any {
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return map[string]any{"files": 0, "size_mb": 0.0}
	}
	var count int
	var totalBytes int64
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".bin") {
			count++
			if info, err := e.Info(); err == nil {
				totalBytes += info.Size()
			}
		}
	}
	return map[string]any{
		"files":   count,
		"size_mb": float64(totalBytes) / (1024 * 1024),
	}
}
