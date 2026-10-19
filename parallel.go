package main

import "sync"

// parallelFor runs fn(0)…fn(n-1) on at most workers goroutines and waits
// for them all. Each call gets its own index, so a caller that writes
// results into a slice indexed by i needs no lock: no two calls touch the
// same slot. The worker count is capped at n, so a short list does not
// start goroutines that will never get work.
//
// This is the shape the app's bounded background work shares — the
// metadata pass of a network listing, the digests of a duplicate scan —
// and keeping it in one place is what stops each new caller from growing
// its own channel-and-WaitGroup copy.
func parallelFor(n, workers int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	if workers == 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}
