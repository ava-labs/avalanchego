package network

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"math/rand"
	"testing"
	"time"
)

func TestGzipCompressor_Compress(t *testing.T) {
	data := RandomString(10000)
	dataBytes := []byte(data)
	dataBytesLength := len(dataBytes)

	compressor := NewCompressor()
	compressedBytes, err := compressor.Compress(dataBytes)
	if err != nil {
		t.Fatal(err)
	}
	compressedBytesLength := len(compressedBytes)

	if compressedBytesLength >= dataBytesLength {
		t.Fatalf("Compressed data should be smaller than data, cLen=%d, len=%d", compressedBytesLength, len(data))
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
	cmpBytes []byte
	pool     = NewCompressorPool()
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
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes := standardCompress(sBytes, gzip.DefaultCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestCompGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes := standardCompress(sBytes, gzip.BestCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestSpeedGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(4096)
		sBytes := []byte(s)
		t1 := time.Now()
		cmpBytes := standardCompress(sBytes, gzip.BestSpeed)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(sBytes)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
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

func standardDecompress(msg []byte) []byte {
	bReader := bytes.NewReader(msg)
	reader, err := gzip.NewReader(bReader)
	if err != nil {
		fmt.Printf("Error when creating gzip reader: %s\n", err)
		return nil
	}

	b, err := ioutil.ReadAll(reader)
	if err != nil {
		fmt.Printf("Error when reading gzipped data: %s\n", err)
		return nil
	}

	err = reader.Close()
	if err != nil {
		fmt.Printf("Error when closing gzip reader: %s\n", err)
		return nil
	}
	return b
}

func RandomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

func TestStandardCompressDecompress(t *testing.T) {
	s := RandomString(4096)
	sBytes := []byte(s)
	cmpBytes := standardCompress(sBytes, gzip.DefaultCompression)
	dcmpBytes := standardDecompress(cmpBytes)
	dcmpString := string(dcmpBytes)
	if dcmpString != s {
		t.Fatalf("decompressed string not equal to original string\ndecomp string: \t `%s`\noriginal string: \t `%s`\n", dcmpString, s)
	}
}
