package main

import (
	"math"
	"math/rand"
	"sort"
	"time"
)

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	s := 0.0
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(xs)-1))
}

func percentileMs(ds []time.Duration, q float64) float64 {
	if len(ds) == 0 {
		return 0
	}
	c := append([]time.Duration(nil), ds...)
	sort.Slice(c, func(a, b int) bool { return c[a] < c[b] })
	i := int(math.Ceil(q*float64(len(c)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(c) {
		i = len(c) - 1
	}
	return float64(c[i].Microseconds()) / 1000
}

// pairedT returns the paired t statistic and two-sided p-value for diffs.
// Zero variance with zero mean is "no difference" (p = 1).
func pairedT(diffs []float64) (t, p float64) {
	n := len(diffs)
	if n < 2 {
		return 0, 1
	}
	m, sd := mean(diffs), stddev(diffs)
	if sd == 0 {
		if m == 0 {
			return 0, 1
		}
		return math.Inf(int(math.Copysign(1, m))), 0
	}
	t = m / (sd / math.Sqrt(float64(n)))
	df := float64(n - 1)
	p = regIncBeta(df/2, 0.5, df/(df+t*t))
	return t, p
}

// regIncBeta is the regularized incomplete beta I_x(a,b) via a continued
// fraction (modified Lentz). Two-sided Student-t p = I_{df/(df+t²)}(df/2, 1/2).
func regIncBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lbeta, _ := math.Lgamma(a + b)
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	front := math.Exp(lbeta - la - lb + a*math.Log(x) + b*math.Log(1-x))
	if x > (a+1)/(a+b+2) {
		return 1 - regIncBeta(b, a, 1-x)
	}
	const tiny = 1e-300
	f, c, d := 1.0, 1.0, 0.0
	for i := 0; i <= 400; i++ {
		m := float64(i / 2)
		var num float64
		switch {
		case i == 0:
			num = 1
		case i%2 == 0:
			num = m * (b - m) * x / ((a + 2*m - 1) * (a + 2*m))
		default:
			num = -(a + m) * (a + b + m) * x / ((a + 2*m) * (a + 2*m + 1))
		}
		d = 1 + num*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		d = 1 / d
		c = 1 + num/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		cd := c * d
		f *= cd
		if math.Abs(1-cd) < 1e-12 {
			break
		}
	}
	return front * (f - 1) / a
}

// bootstrapCI is the 95% percentile bootstrap interval of the mean of paired
// differences, from a fixed seed so every report is reproducible. It does not
// assume normality, which placement points (10/6/3/1) badly violate.
func bootstrapCI(diffs []float64, resamples int, seed int64) [2]float64 {
	n := len(diffs)
	if n == 0 || resamples <= 0 {
		return [2]float64{}
	}
	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, resamples)
	for r := range means {
		sum := 0.0
		for i := 0; i < n; i++ {
			sum += diffs[rng.Intn(n)]
		}
		means[r] = sum / float64(n)
	}
	sort.Float64s(means)
	lo := int(0.025 * float64(resamples))
	hi := int(math.Ceil(0.975*float64(resamples))) - 1
	if hi >= resamples {
		hi = resamples - 1
	}
	return [2]float64{means[lo], means[hi]}
}
