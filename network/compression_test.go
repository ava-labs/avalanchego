package network

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func TestCompressDecompress(t *testing.T) {
	data := RandomString(10000)
	dataBytes := []byte(data)

	compressor := NewCompressor()
	compressedBytes, err := compressor.Compress(dataBytes)
	if err != nil {
		t.Fatal(err)
	}

	decompressor := NewDecompressor()
	decompressedBytes, err := decompressor.Decompress(compressedBytes)
	if err != nil {
		t.Fatal(err)
	}

	decompressedData := string(decompressedBytes)
	if decompressedData != data {
		t.Fatalf("decompressed string must equal original string:\ndecomp:`%s`\noriginal:`%s`", decompressedData, data)
	}
}

func BenchmarkGzip(b *testing.B) {
	b.Run("standard gzip", BenchmarkStandardGzip)
	b.Run("best compression gzip", BenchmarkBestCompGzip)
	b.Run("best speed gzip", BenchmarkBestSpeedGzip)
}

func BenchmarkPooledVsNonPooledGzip(b *testing.B) {
	b.Run("PooledGzip", BenchmarkPooledGzip)
	b.Run("NonPooledGzip", BenchmarkStandardGzipCompression)
}

var (
	cmpBytes  []byte
	dcmpBytes []byte
	pool      = NewCompressorPool()
)

func BenchmarkPooledGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		w := pool.Get().(Compressor)
		cmpBytes, err := w.Compress(sBytes)
		if err != nil {
			b.Fatal(err)
		}
		w.Reset()
		pool.Put(w)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")
	}
}

func BenchmarkStandardGzipCompression(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes = standardCompress(sBytes, gzip.DefaultCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")
	}
}

func BenchmarkStandardGzip(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes = standardCompress(sBytes, gzip.DefaultCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		dcmpBytes, err = standardDecompress(cmpBytes)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestCompGzip(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes = standardCompress(sBytes, gzip.BestCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		dcmpBytes, err = standardDecompress(cmpBytes)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestSpeedGzip(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes = standardCompress(sBytes, gzip.BestSpeed)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		dcmpBytes, err = standardDecompress(cmpBytes)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func standardCompress(msg []byte, level int) []byte {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		fmt.Printf("Error when creating gzip writer: %s\n", err)
		return nil
	}

	_, err = writer.Write(msg)
	if err != nil {
		fmt.Printf("Error when writing bytes to gzip: %s\n", err)
		return nil
	}

	err = writer.Flush()
	if err != nil {
		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
		return nil
	}

	err = writer.Close()
	if err != nil {
		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
		return nil
	}
	return buf.Bytes()
}

func standardDecompress(msg []byte) ([]byte, error) {
	decompressor := NewDecompressor()

	b, err := decompressor.Decompress(msg)
	return b, err
}

func RandomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")

	s := make([]rune, n)
	for i := range s {
		randIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			panic(err)
		}

		s[i] = letters[randIndex.Int64()]
	}
	return string(s)
}
