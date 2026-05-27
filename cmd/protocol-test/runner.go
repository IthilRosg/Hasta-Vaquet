package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TestResult struct {
	Name      string  `json:"name"`
	Genome    Genome  `json:"genome"`
	StartAt   string  `json:"start_at"`
	LossPct   float64 `json:"loss_pct"`
	BestSize  int     `json:"best_size"`
	Score     float64 `json:"score"`
}

func RunTest(name string, g Genome, resultsDir string, mu *sync.Mutex, results *[]TestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	res := TestResult{Name: name, Genome: g, StartAt: time.Now().Format(time.RFC3339)}

	// Test ICMP loss at different sizes (200, 500, 800, 1100, 1400)
	sizes := []int{200, 500, 800, 1100, 1400}
	bestSize := 1400
	bestLoss := 100.0

	for _, sz := range sizes {
		l, _ := pingSize(serverIP, sz, 15)
		log.Printf("[%s] size=%d loss=%.1f%%", name, sz, l)
		if l < bestLoss {
			bestLoss = l
			bestSize = sz
		}
		time.Sleep(500 * time.Millisecond)
	}

	res.LossPct = bestLoss
	res.BestSize = bestSize

	// Score: 0% loss = 1000, each % loss = -100
	res.Score = 1000.0 - bestLoss*10
	if res.Score < 0 { res.Score = 0 }

	saveAndLog(res, resultsDir, mu, results)
}

func saveAndLog(r TestResult, dir string, mu *sync.Mutex, results *[]TestResult) {
	data, _ := json.MarshalIndent(r, "", "  ")
	fn := fmt.Sprintf("%s.json", r.Name)
	os.WriteFile(dir+"/"+fn, data, 0644)
	log.Printf("[RESULT] %s: loss=%.1f%% best_size=%d fec=%d score=%.0f",
		r.Name, r.LossPct, r.BestSize, r.Genome.FEC, r.Score)
	mu.Lock()
	*results = append(*results, r)
	mu.Unlock()
}

func pingSize(host string, size, count int) (lossPct, avgMs float64) {
	ps := fmt.Sprintf(`$r=Test-Connection %s -Count %d -BufferSize %d -ErrorAction SilentlyContinue;if(-not $r){'100:0';return};$l=%d-$r.Count;$p=($l*100)/%d;$a=($r|Measure-Object -Property ResponseTime -Average).Average;''+[math]::Round($p,1)+':'+[math]::Round($a,1)`,
		host, count, size, count, count)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil { return 100, 0 }
	s := strings.TrimSpace(string(out))
	parts := strings.Split(s, ":")
	if len(parts) >= 2 {
		l, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		m, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if e1 == nil && e2 == nil { return l, m }
	}
	return 100, 0
}
