// Command loadgen keeps a fixed number of requests in flight against the
// Rayls prover service for a fixed time (a closed loop), records the latency
// of every request, and samples the server process's resident memory.
//
// Each worker sends a request, waits for the answer, and immediately sends the
// next one. Requests that start during the warm-up period are discarded.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sample struct {
	start time.Time
	dur   time.Duration
	ok    bool
}

func rssKB(pid int) int64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				v, _ := strconv.ParseInt(f[1], 10, 64)
				return v
			}
		}
	}
	return -1
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	i := int(p*float64(len(d))+0.999999) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(d) {
		i = len(d) - 1
	}
	return d[i]
}

func ms(d time.Duration) string { return fmt.Sprintf("%.0fms", float64(d)/1e6) }

func main() {
	url := flag.String("url", "http://localhost:3003/generateProofTransfer-6", "endpoint")
	bodyPath := flag.String("body", "", "request body file")
	conc := flag.Int("c", 1, "requests in flight")
	dur := flag.Duration("d", 60*time.Second, "measured duration")
	warm := flag.Duration("warmup", 5*time.Second, "discarded warm-up")
	pid := flag.Int("pid", 0, "server pid for memory sampling")
	window := flag.Duration("window", 0, "if set, also report per-window throughput, p99 and peak RSS")
	label := flag.String("label", "", "label printed with results")
	flag.Parse()

	body, err := os.ReadFile(*bodyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{MaxIdleConnsPerHost: *conc + 4}}

	t0 := time.Now()
	measureFrom := t0.Add(*warm)
	stop := measureFrom.Add(*dur)

	var mu sync.Mutex
	var samples []sample
	var wg sync.WaitGroup
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				st := time.Now()
				resp, err := client.Post(*url, "application/json", bytes.NewReader(body))
				ok := err == nil
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					ok = resp.StatusCode == 200
				}
				el := time.Since(st)
				mu.Lock()
				samples = append(samples, sample{st, el, ok})
				mu.Unlock()
			}
		}()
	}

	// memory sampler, every 100 ms
	type memPt struct {
		t  time.Time
		kb int64
	}
	var mem []memPt
	done := make(chan struct{})
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case t := <-tk.C:
				if *pid > 0 {
					mem = append(mem, memPt{t, rssKB(*pid)})
				}
			}
		}
	}()
	wg.Wait()
	close(done)
	end := time.Now()

	// Only requests that started inside the measured window count. Requests
	// that started before the stop time but finished after it are included,
	// so the effective window is [measureFrom, last completion].
	var lat []time.Duration
	errs := 0
	lastEnd := measureFrom
	for _, s := range samples {
		if s.start.Before(measureFrom) {
			continue
		}
		if !s.ok {
			errs++
			continue
		}
		lat = append(lat, s.dur)
		if e := s.start.Add(s.dur); e.After(lastEnd) {
			lastEnd = e
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	span := lastEnd.Sub(measureFrom).Seconds()
	var peak, sum int64
	n := 0
	for _, m := range mem {
		if m.t.Before(measureFrom) || m.kb < 0 {
			continue
		}
		if m.kb > peak {
			peak = m.kb
		}
		sum += m.kb
		n++
	}
	avg := int64(0)
	if n > 0 {
		avg = sum / int64(n)
	}
	fmt.Printf("RESULT load %s c=%d n=%d errors=%d span=%.1fs throughput=%.3f/s p50=%s p95=%s p99=%s max=%s rss_peak=%dMB rss_avg=%dMB\n",
		*label, *conc, len(lat), errs, span, float64(len(lat))/span,
		ms(pct(lat, .50)), ms(pct(lat, .95)), ms(pct(lat, .99)), ms(pct(lat, 1)), peak/1024, avg/1024)

	if *window > 0 {
		for ws := measureFrom; ws.Before(end); ws = ws.Add(*window) {
			we := ws.Add(*window)
			var wl []time.Duration
			for _, s := range samples {
				e := s.start.Add(s.dur)
				if s.ok && !s.start.Before(measureFrom) && !e.Before(ws) && e.Before(we) {
					wl = append(wl, s.dur)
				}
			}
			sort.Slice(wl, func(i, j int) bool { return wl[i] < wl[j] })
			var wp int64
			for _, m := range mem {
				if !m.t.Before(ws) && m.t.Before(we) && m.kb > wp {
					wp = m.kb
				}
			}
			wlen := window.Seconds()
			if lastEnd.Before(we) {
				wlen = lastEnd.Sub(ws).Seconds()
			}
			if wlen <= 0 {
				continue
			}
			fmt.Printf("RESULT window %s c=%d t=%3.0fs len=%.0fs completed=%d throughput=%.3f/s p50=%s p99=%s rss_peak=%dMB\n",
				*label, *conc, ws.Sub(measureFrom).Seconds(), wlen, len(wl), float64(len(wl))/wlen,
				ms(pct(wl, .5)), ms(pct(wl, .99)), wp/1024)
		}
	}
}
