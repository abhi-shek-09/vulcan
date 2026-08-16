package scheduler

import "fmt"

func RequiredWorkers(rps, capacityRPS int) (int, error) {
	if rps <= 0 || capacityRPS <= 0 {
		return 0, fmt.Errorf("rps and worker capacity must be greater than zero")
	}
	return (rps + capacityRPS - 1) / capacityRPS, nil
}

func DistributeRPS(totalRPS, workerCount int) ([]int, error) {
	if totalRPS <= 0 || workerCount <= 0 {
		return nil, fmt.Errorf("rps and worker count must be greater than zero")
	}
	base := totalRPS / workerCount
	remainder := totalRPS % workerCount
	allocations := make([]int, workerCount)
	for i := range allocations {
		allocations[i] = base
		if i < remainder {
			allocations[i]++
		}
	}
	return allocations, nil
}
