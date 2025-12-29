# Bolt's Journal

## 2025-02-23 - Sequential API calls in hot loop
**Learning:** `monitorPositions` in `ExecutorFast` was performing sequential RPC and Jupiter API calls for every active position. This creates a linear performance degradation O(n) where n is the number of positions.
**Action:** Parallelized the loop using `sync.WaitGroup` and a semaphore to limit concurrency, changing the time complexity to O(n/k) where k is the concurrency limit (or bounded by the slowest request).
