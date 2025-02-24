package statistics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
)

type StatisticsConfig struct {
	LogInterval string
}

// NewStatistics creates a new instance of Statistics.
func NewStatistics(statisticsConfig *StatisticsConfig, bundleTypes []string) *Statistics {
	statistics := &Statistics{
		log:           ctrl.Log.WithName("statistics"),
		bundleMetrics: make(map[string]*bundleMetrics),
		logInterval:   statisticsConfig.LogInterval,
	}

	for _, bundleType := range bundleTypes {
		statistics.bundleMetrics[bundleType] = newBundleMetrics()
	}

	return statistics
}

// Statistics aggregates different statistics.
type Statistics struct {
	log                      logr.Logger
	numOfAvailableDBWorkers  int
	conflationReadyQueueSize int
	numOfConflationUnits     int
	bundleMetrics            map[string]*bundleMetrics
	logInterval              string
	mutex                    sync.Mutex
}

// IncrementNumberOfReceivedBundles increments total number of received bundles of the specific type via transport.
// if bundle type is not registered, do nothing
func (s *Statistics) IncrementNumberOfReceivedBundles(b bundle.ManagerBundle) {
	bundleMetrics, ok := s.bundleMetrics[bundle.GetBundleType(b)]
	if !ok {
		return
	}
	bundleMetrics.totalReceived++
}

// SetNumberOfAvailableDBWorkers sets number of available db workers.
func (s *Statistics) SetNumberOfAvailableDBWorkers(numOf int) {
	s.numOfAvailableDBWorkers = numOf
}

// SetConflationReadyQueueSize sets conflation ready queue size.
func (s *Statistics) SetConflationReadyQueueSize(size int) {
	s.conflationReadyQueueSize = size
}

// StartConflationUnitMetrics starts conflation unit metrics of the specific bundle type.
func (s *Statistics) StartConflationUnitMetrics(b bundle.ManagerBundle) {
	bundleMetrics, ok := s.bundleMetrics[bundle.GetBundleType(b)]
	if !ok {
		return
	}
	bundleMetrics.conflationUnit.start(b.GetLeafHubName())
}

// StopConflationUnitMetrics stops conflation unit metrics of the specific bundle type.
func (s *Statistics) StopConflationUnitMetrics(b bundle.ManagerBundle, err error) {
	bundleMetrics, ok := s.bundleMetrics[bundle.GetBundleType(b)]
	if !ok {
		return
	}
	bundleMetrics.conflationUnit.stop(b.GetLeafHubName(), err)
}

// IncrementNumberOfConflations increments number of conflations
func (s *Statistics) IncrementNumberOfConflations() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.numOfConflationUnits++
}

// AddDatabaseMetrics adds database metrics of the specific bundle type.
func (s *Statistics) AddDatabaseMetrics(b bundle.ManagerBundle, duration time.Duration, err error) {
	bundleMetrics, ok := s.bundleMetrics[bundle.GetBundleType(b)]
	if !ok {
		return
	}
	bundleMetrics.database.add(duration, err)
}

// Start starts the statistics.
func (s *Statistics) Start(ctx context.Context) error {
	s.log.Info("starting statistics")
	duration, err := time.ParseDuration(s.logInterval)
	if err != nil {
		return err
	}

	go s.run(ctx, duration)

	// blocking wait until getting cancel context event
	<-ctx.Done()
	s.log.Info("stopped statistics")

	return nil
}

func (s *Statistics) run(ctx context.Context, duration time.Duration) {
	if duration.Seconds() <= 0 {
		return // if log interval is set to 0 or negative value, statistics log is disabled.
	}

	ticker := time.NewTicker(duration)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return

		case <-ticker.C: // dump statistics
			var stringBuilder strings.Builder
			success := int64(0)
			fail := int64(0)
			storageAvg := float64(0)
			conflationAvg := float64(0)

			for bundleType, bundleMetrics := range s.bundleMetrics {
				stringBuilder.WriteString(fmt.Sprintf("[%-42s(%d) | conflation(%-42s) | storage(%-42s)] \n",
					bundleType, bundleMetrics.totalReceived,
					bundleMetrics.conflationUnit.toString(),
					bundleMetrics.database.toString()))
				success += bundleMetrics.totalReceived
				fail += (bundleMetrics.conflationUnit.failures + bundleMetrics.database.failures)
				if bundleMetrics.conflationUnit.successes > 0 {
					conflationAvg = float64(bundleMetrics.conflationUnit.totalDuration / bundleMetrics.conflationUnit.successes)
				}
				if bundleMetrics.database.successes > 0 {
					storageAvg = float64(bundleMetrics.database.totalDuration / bundleMetrics.database.successes)
				}
			}
			metrics := fmt.Sprintf("{CU=%d, CUQueue=%d, idleDBW=%d, success=%d, fail=%d, CU Avg=%.0f ms, DB Avg=%.0f ms}",
				s.numOfConflationUnits, s.conflationReadyQueueSize, s.numOfAvailableDBWorkers, success, fail,
				conflationAvg, storageAvg)

			s.log.V(4).Info(fmt.Sprintf("%s\n%s", metrics, stringBuilder.String()))
		}
	}
}
