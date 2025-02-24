package statistics

// bundleMetrics aggregates metrics per specific bundle type.
type bundleMetrics struct {
	conflationUnit conflationUnitMetrics // measures a time and conflations while bundle waits in CU's priority queue
	database       genericMetrics        // measures a time took by db worker to process bundle
	totalReceived  int64                 // total received bundles of the specific type via transport
}

func newBundleMetrics() *bundleMetrics {
	return &bundleMetrics{conflationUnit: conflationUnitMetrics{
		startTimestamps: make(map[string]int64),
	}}
}
