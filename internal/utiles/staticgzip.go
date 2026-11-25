package utiles

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// 静态资源 gzip 中间件。
//
// 前端是编译期嵌进二进制的（//go:embed），go-zero 1.8 的 rest 没有内置压缩，
// 结果 400 KB 的 JS 原样传输。资源文件名带 hash 且内容不变，所以压缩结果可以永久缓存。
var gzipCache sync.Map // path -> gzipEntry

// 压缩结果必须连同内容类型一起缓存：
// 之前只存字节、命中时按扩展名猜类型，导致 /manager（无扩展名）被当成 text/plain，
// 浏览器把 HTML 当源码显示。
type gzipEntry struct {
	data        []byte
	contentType string
}

// GzipStatic 对文本类静态资源做 gzip；不支持压缩的请求/类型原样透传。
func GzipStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		if cached, ok := gzipCache.Load(r.URL.Path); ok {
			entry := cached.(gzipEntry)
			writeGzip(w, entry.data, entry.contentType)
			return
		}

		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		result := rec.Result()
		body := rec.Body.Bytes()

		contentType := result.Header.Get("Content-Type")
		if result.StatusCode == http.StatusOK && len(body) > 1024 && compressible(contentType) {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			if _, err := zw.Write(body); err != nil {
				_ = zw.Close()
				copyHeader(w, result.Header)
				w.WriteHeader(result.StatusCode)
				_, _ = w.Write(body)
				return
			}
			_ = zw.Close()
			compressed := buf.Bytes()
			gzipCache.Store(r.URL.Path, gzipEntry{data: compressed, contentType: contentType})

			copyHeader(w, result.Header)
			writeGzip(w, compressed, contentType)
			return
		}

		copyHeader(w, result.Header)
		w.WriteHeader(result.StatusCode)
		_, _ = w.Write(body)
	})
}

func writeGzip(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func copyHeader(w http.ResponseWriter, h http.Header) {
	for k, values := range h {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
}

func compressible(contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "text/"),
		strings.Contains(ct, "javascript"),
		strings.Contains(ct, "json"),
		strings.Contains(ct, "svg"):
		return true
	}
	return false
}
