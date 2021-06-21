package network

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressDecompress(t *testing.T) {
	data := []byte(randomString(1000))

	compressor := NewCompressor()
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)
}

// func BenchmarkGzip(b *testing.B) {
// 	b.Run("standard gzip", BenchmarkStandardGzip)
// 	b.Run("best compression gzip", BenchmarkBestCompGzip)
// 	b.Run("best speed gzip", BenchmarkBestSpeedGzip)
// }

// var (
// 	cmpBytes  []byte
// 	dcmpBytes []byte
// )

// func BenchmarkStandardGzipCompression(b *testing.B) {
// 	for i := 0; i < b.N; i++ {
// 		s := randomString(4096)
// 		sBytes := []byte(s)
// 		t1 := time.Now()
// 		cmpBytes = standardCompress(sBytes, gzip.DefaultCompression)
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
// 		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")
// 	}
// }

// func BenchmarkStandardGzip(b *testing.B) {
// 	var err error
// 	for i := 0; i < b.N; i++ {
// 		s := randomString(4096)
// 		sBytes := []byte(s)
// 		t1 := time.Now()
// 		cmpBytes = standardCompress(sBytes, gzip.DefaultCompression)
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
// 		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

// 		t1 = time.Now()
// 		dcmpBytes, err = standardDecompress(cmpBytes)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
// 	}
// }

// func BenchmarkBestCompGzip(b *testing.B) {
// 	var err error
// 	for i := 0; i < b.N; i++ {
// 		s := randomString(4096)
// 		sBytes := []byte(s)
// 		t1 := time.Now()
// 		cmpBytes = standardCompress(sBytes, gzip.BestCompression)
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
// 		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

// 		t1 = time.Now()
// 		dcmpBytes, err = standardDecompress(cmpBytes)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
// 	}
// }

// func BenchmarkBestSpeedGzip(b *testing.B) {
// 	var err error
// 	for i := 0; i < b.N; i++ {
// 		s := randomString(4096)
// 		sBytes := []byte(s)
// 		t1 := time.Now()
// 		cmpBytes = standardCompress(sBytes, gzip.BestSpeed)
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
// 		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

// 		t1 = time.Now()
// 		dcmpBytes, err = standardDecompress(cmpBytes)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
// 	}
// }

// func standardCompress(msg []byte, level int) []byte {
// 	var buf bytes.Buffer
// 	writer, err := gzip.NewWriterLevel(&buf, level)
// 	if err != nil {
// 		fmt.Printf("Error when creating gzip writer: %s\n", err)
// 		return nil
// 	}

// 	_, err = writer.Write(msg)
// 	if err != nil {
// 		fmt.Printf("Error when writing bytes to gzip: %s\n", err)
// 		return nil
// 	}

// 	err = writer.Flush()
// 	if err != nil {
// 		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
// 		return nil
// 	}

// 	err = writer.Close()
// 	if err != nil {
// 		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
// 		return nil
// 	}
// 	return buf.Bytes()
// }

func randomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")

	s := make([]rune, n)
	for i := range s {
		randIndex := rand.Intn(len(letters))
		s[i] = letters[randIndex]
	}
	return string(s)
}
