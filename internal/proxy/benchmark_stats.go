package proxy

import (
	"math"
	"time"
)

// Fixed storage keeps duration-mode tests from retaining millions of samples.
// Percentile buckets have at most ~1% relative error (microsecond resolution).
type benchmarkStats struct {
	result    BenchmarkResult
	histogram [2048]uint64
	responses uint64
	total     time.Duration
}

func newBenchmarkStats(target BenchmarkTarget, planned int) *benchmarkStats {
	return &benchmarkStats{result: BenchmarkResult{Key: target.Key, Name: target.Name, Source: target.Source, Planned: planned,
		StatusCodes: map[int]int{}, Errors: map[string]int{}}}
}

func (s *benchmarkStats) add(sample benchmarkSample) {
	r := &s.result
	r.Completed++
	r.Bytes += sample.bytes
	if sample.err != nil {
		if sample.cancelled {
			r.CancelledRequests++
			return
		}
		r.NetworkFailures++
		r.Errors[benchmarkError(sample.err)]++
		return
	}
	r.StatusCodes[sample.status]++
	if sample.status >= 200 && sample.status < 300 {
		r.Success++
	} else {
		r.HTTPFailures++
	}
	s.responses++
	s.total += sample.duration
	if s.responses == 1 || sample.duration < r.Fastest {
		r.Fastest = sample.duration
	}
	if sample.duration > r.Slowest {
		r.Slowest = sample.duration
	}
	index := int(math.Log(float64(max(1, sample.duration.Microseconds()))) / math.Log(1.01))
	s.histogram[min(index, len(s.histogram)-1)]++
}

func (s *benchmarkStats) snapshot(elapsed time.Duration, finished bool) BenchmarkResult {
	r := s.result
	r.Elapsed, r.Finished = elapsed, finished
	r.StatusCodes = make(map[int]int, len(s.result.StatusCodes))
	for key, value := range s.result.StatusCodes {
		r.StatusCodes[key] = value
	}
	r.Errors = make(map[string]int, len(s.result.Errors))
	for key, value := range s.result.Errors {
		r.Errors[key] = value
	}
	if s.responses > 0 {
		r.Average = s.total / time.Duration(s.responses)
		r.P50, r.P90, r.P95, r.P99 = s.percentile(.50), s.percentile(.90), s.percentile(.95), s.percentile(.99)
	}
	if elapsed > 0 {
		r.RequestsPerSecond = float64(r.Completed-r.CancelledRequests) / elapsed.Seconds()
		r.BytesPerSecond = float64(r.Bytes) / elapsed.Seconds()
	}
	return r
}

func (s *benchmarkStats) percentile(fraction float64) time.Duration {
	threshold := uint64(math.Ceil(float64(s.responses) * fraction))
	var count uint64
	for index, value := range s.histogram {
		count += value
		if count >= threshold {
			duration := time.Duration(math.Pow(1.01, float64(index+1)) * float64(time.Microsecond))
			return min(max(duration, s.result.Fastest), s.result.Slowest)
		}
	}
	return s.result.Slowest
}
