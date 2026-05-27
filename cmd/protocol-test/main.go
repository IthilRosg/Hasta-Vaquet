package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"
)

var serverIP = "45.134.39.18"
var resultsDir = "results"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	os.MkdirAll(resultsDir, 0755)
	if len(os.Args) > 1 { serverIP = os.Args[1] }

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Gen 0 seeds
	genomes := []Genome{
		{Name: "baseline", FEC: 1},
		{Name: "fec2", FEC: 2},
		{Name: "fec3", FEC: 3},
		{Name: "delay10-50", FEC: 1, MinDelayMs: 10, MaxDelayMs: 50},
		{Name: "port+10", FEC: 1, PortOffset: 10},
		{Name: "port-10", FEC: 1, PortOffset: -10},
	}

	var allMu sync.Mutex
	var allResults []TestResult

	for gen := 0; gen < 3; gen++ {
		log.Printf("\n=== Gen %d: %d genomes ===", gen, len(genomes))

		var wg sync.WaitGroup
		var mu sync.Mutex
		var results []TestResult

		for _, g := range genomes {
			wg.Add(1)
			g := g
			go func() {
				RunTest(g.Name, g, resultsDir, &mu, &results, &wg)
			}()
			time.Sleep(200 * time.Millisecond)
		}
		wg.Wait()

		sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

		allMu.Lock()
		allResults = append(allResults, results...)
		allMu.Unlock()

		log.Printf("=== Top 3 Gen %d ===", gen)
		for i := 0; i < 3 && i < len(results); i++ {
			r := results[i]
			log.Printf("#%d: %s loss=%.1f%% best_size=%d score=%.0f",
				i+1, r.Name, r.LossPct, r.BestSize, r.Score)
		}

		if gen < 2 {
			top := results
			if len(top) > 3 { top = top[:3] }
			genomes = nextGen(top, rng)
		}
	}

	// Final results
	sort.Slice(allResults, func(i, j int) bool { return allResults[i].Score > allResults[j].Score })
	log.Printf("\n=== FINAL RESULTS ===")
	for i, r := range allResults {
		if i >= 5 { break }
		log.Printf("#%d: %s loss=%.1f%% best_size=%d fec=%d score=%.0f",
			i+1, r.Name, r.LossPct, r.BestSize, r.Genome.FEC, r.Score)
	}
	log.Println("Done")
}

func nextGen(top []TestResult, rng *rand.Rand) []Genome {
	var next []Genome
	next = append(next, top[0].Genome)
	if len(top) > 1 { next = append(next, top[1].Genome) }
	for i := 0; i < 4; i++ {
		m := mutate(top[0].Genome, rng)
		m.Name = fmt.Sprintf("m%d", i)
		next = append(next, m)
	}
	for i := 0; i < 2; i++ {
		c := crossover(top[0].Genome, top[1%len(top)].Genome, rng)
		c.Name = fmt.Sprintf("x%d", i)
		next = append(next, c)
	}
	return next
}

func mutate(g Genome, rng *rand.Rand) Genome {
	n := g
	switch rng.Intn(6) {
	case 0: n.FEC = 1 + rng.Intn(4)
	case 1: n.MinDelayMs = rng.Intn(100); n.MaxDelayMs = n.MinDelayMs + rng.Intn(200)
	case 2: n.IdleBps = rng.Intn(500000)
	case 3: n.PortOffset = rng.Intn(200) - 100
	case 4: n.Mimic = []string{"", "tls"}[rng.Intn(2)]
	}
	return n
}

func crossover(a, b Genome, rng *rand.Rand) Genome {
	c := a
	if rng.Intn(2) == 0 { c.FEC = b.FEC }
	if rng.Intn(2) == 0 { c.MinDelayMs = b.MinDelayMs }
	if rng.Intn(2) == 0 { c.MaxDelayMs = b.MaxDelayMs }
	if rng.Intn(2) == 0 { c.IdleBps = b.IdleBps }
	if rng.Intn(2) == 0 { c.Mimic = b.Mimic }
	if rng.Intn(2) == 0 { c.PortOffset = b.PortOffset }
	return c
}
