// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dbsyncer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/config"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/nonk8sapi"
	managerscheme "github.com/stolostron/multicluster-global-hub/manager/pkg/scheme"
	sycner "github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer"
	specsycner "github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer/db2transport/syncer"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	commonobjects "github.com/stolostron/multicluster-global-hub/pkg/objects"
	"github.com/stolostron/multicluster-global-hub/pkg/statistics"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/consumer"
	genericproducer "github.com/stolostron/multicluster-global-hub/pkg/transport/producer"
	"github.com/stolostron/multicluster-global-hub/test/pkg/testpostgres"
)

var (
	testenv         *envtest.Environment
	cfg             *rest.Config
	ctx             context.Context
	cancel          context.CancelFunc
	mgr             ctrl.Manager
	kubeClient      client.Client
	testPostgres    *testpostgres.TestPostgres
	genericConsumer *consumer.GenericConsumer
	producer        transport.Producer
)

func TestSpecSyncer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spec Syncer Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	Expect(os.Setenv("POD_NAMESPACE", "default")).To(Succeed())

	ctx, cancel = context.WithCancel(context.Background())

	By("Prepare envtest environment")
	var err error
	testenv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "..", "pkg", "testdata", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err = testenv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	By("Create test postgres")
	testPostgres, err = testpostgres.NewTestPostgres()
	Expect(err).NotTo(HaveOccurred())
	err = database.InitGormInstance(&database.DatabaseConfig{
		URL:        testPostgres.URI,
		Dialect:    database.PostgresDialect,
		CaCertPath: "ca-cert-path",
		PoolSize:   5,
	})
	Expect(err).NotTo(HaveOccurred())

	err = testpostgres.InitDatabase(testPostgres.URI)
	Expect(err).NotTo(HaveOccurred())

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		MetricsBindAddress: "0",
		Scheme:             scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	By("Add to Scheme")
	managerscheme.AddToScheme(mgr.GetScheme())

	By("Get kubeClient")
	kubeClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(kubeClient).NotTo(BeNil())

	managerConfig := &config.ManagerConfig{
		SyncerConfig: &config.SyncerConfig{
			SpecSyncInterval:              1 * time.Second,
			DeletedLabelsTrimmingInterval: 2 * time.Second,
		},
		TransportConfig: &transport.TransportConfig{
			TransportType:     string(transport.Chan),
			CommitterInterval: 10 * time.Second,
		},
		StatisticsConfig:      &statistics.StatisticsConfig{},
		NonK8sAPIServerConfig: &nonk8sapi.NonK8sAPIServerConfig{},
		ElectionConfig:        &commonobjects.LeaderElectionConfig{},
	}
	producer, err = genericproducer.NewGenericProducer(managerConfig.TransportConfig)
	Expect(err).NotTo(HaveOccurred())

	Expect(specsycner.AddDB2TransportSyncers(mgr, managerConfig, producer)).Should(Succeed())
	Expect(specsycner.AddManagedClusterLabelSyncer(mgr,
		managerConfig.SyncerConfig.DeletedLabelsTrimmingInterval)).Should(Succeed())

	// mock consume message from agent
	By("Create kafka consumer")
	genericConsumer, err = consumer.NewGenericConsumer(managerConfig.TransportConfig)
	Expect(err).NotTo(HaveOccurred())
	Expect(mgr.Add(genericConsumer)).Should(Succeed())

	err = sycner.SendSyncAllMsgInfo(producer)
	Expect(err).NotTo(HaveOccurred())

	By("Start the manager")
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).ToNot(HaveOccurred(), "failed to run manager")
	}()

	By("Waiting for the manager to be ready")
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
})

var _ = AfterSuite(func() {
	cancel()
	Expect(testPostgres.Stop()).NotTo(HaveOccurred())

	By("Tearing down the test environment")
	err := testenv.Stop()
	// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	// Set 4 with random
	if err != nil {
		time.Sleep(4 * time.Second)
	}
	Expect(testenv.Stop()).NotTo(HaveOccurred())
})
