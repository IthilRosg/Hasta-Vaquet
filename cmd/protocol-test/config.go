package main

import "math/rand"

type Genome struct {
	Name string

	// FEC — packet duplication factor (1=off, 2=every packet 2x, etc)
	FEC int

	// Fixed packet sizes: 0=off, 256/512/1024/1300
	PadTargetSize int

	// Inter-packet delay (ms)
	MinDelayMs int
	MaxDelayMs int

	// Dummy traffic: bytes/sec to send during idle
	IdleBps int

	// First bytes mimic: ""=standard, "tls"=TLS ClientHello prefix
	Mimic string

	// Port offset from base 19999
	PortOffset int
}

func DefaultGenome() Genome {
	return Genome{
		Name:          "baseline",
		FEC:           1,
		PadTargetSize: 1300,
		MinDelayMs:    0,
		MaxDelayMs:    0,
		IdleBps:       0,
		Mimic:         "",
		PortOffset:    0,
	}
}

func Mutate(g Genome, rng *rand.Rand) Genome {
	n := g
	n.Name = "mutated"

	switch rng.Intn(6) {
	case 0:
		n.FEC = 1 + rng.Intn(3)
	case 1:
		sizes := []int{0, 256, 512, 1024, 1300}
		n.PadTargetSize = sizes[rng.Intn(len(sizes))]
	case 2:
		n.MinDelayMs = rng.Intn(50)
		n.MaxDelayMs = n.MinDelayMs + rng.Intn(100)
	case 3:
		n.IdleBps = rng.Intn(500000)
	case 4:
		mimics := []string{"", "tls"}
		n.Mimic = mimics[rng.Intn(len(mimics))]
	case 5:
		n.PortOffset = rng.Intn(1000) - 500
	}
	return n
}

func Crossover(a, b Genome, rng *rand.Rand) Genome {
	c := a
	c.Name = "cross"

	fields := []bool{
		rng.Intn(2) == 0, // FEC
		rng.Intn(2) == 0, // PadTargetSize
		rng.Intn(2) == 0, // MinDelayMs
		rng.Intn(2) == 0, // MaxDelayMs
		rng.Intn(2) == 0, // IdleBps
		rng.Intn(2) == 0, // Mimic
		rng.Intn(2) == 0, // PortOffset
	}

	src := []Genome{a, b}
	if fields[0] { c.FEC = src[rng.Intn(2)].FEC }
	if fields[1] { c.PadTargetSize = src[rng.Intn(2)].PadTargetSize }
	if fields[2] { c.MinDelayMs = src[rng.Intn(2)].MinDelayMs }
	if fields[3] { c.MaxDelayMs = src[rng.Intn(2)].MaxDelayMs }
	if fields[4] { c.IdleBps = src[rng.Intn(2)].IdleBps }
	if fields[5] { c.Mimic = src[rng.Intn(2)].Mimic }
	if fields[6] { c.PortOffset = src[rng.Intn(2)].PortOffset }
	return c
}
