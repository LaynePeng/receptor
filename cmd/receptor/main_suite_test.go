package main_test

import (
	"fmt"
	"net/url"
	"os"

	"github.com/cloudfoundry-incubator/consuladapter"
	"github.com/cloudfoundry-incubator/receptor"
	"github.com/cloudfoundry-incubator/receptor/cmd/receptor/testrunner"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry/gunk/diegonats"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"

	"testing"
	"time"
)

const (
	username = "username"
	password = "password"
)

var natsPort int
var natsAddress string
var natsClient diegonats.NATSClient
var natsServerRunner *ginkgomon.Runner
var natsClientRunner diegonats.NATSClientRunner
var natsGroupProcess ifrit.Process

var etcdPort int
var etcdUrl string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var etcdAdapter storeadapter.StoreAdapter

var consulPort int
var consulRunner consuladapter.ClusterRunner
var consulAdapter consuladapter.Adapter

var bbs *Bbs.BBS

var logger lager.Logger

var client receptor.Client
var receptorBinPath string
var receptorAddress string
var receptorTaskHandlerAddress string
var receptorArgs testrunner.Args
var receptorRunner *ginkgomon.Runner
var receptorProcess ifrit.Process

func TestReceptor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Receptor Cmd Suite")
}

var _ = SynchronizedBeforeSuite(
	func() []byte {
		receptorConfig, err := gexec.Build("github.com/cloudfoundry-incubator/receptor/cmd/receptor", "-race")
		Ω(err).ShouldNot(HaveOccurred())
		return []byte(receptorConfig)
	},
	func(receptorConfig []byte) {
		receptorBinPath = string(receptorConfig)
		SetDefaultEventuallyTimeout(15 * time.Second)

		etcdPort = 4001 + GinkgoParallelNode()
		etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
		etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)

		consulPort = 9001 + config.GinkgoConfig.ParallelNode*consuladapter.PortOffsetLength
		consulRunner = consuladapter.NewClusterRunner(
			consulPort,
			1,
			"http",
		)
		consulAdapter = consulRunner.NewAdapter()

		etcdRunner.Start()
		consulRunner.Start()
	},
)

var _ = SynchronizedAfterSuite(func() {
	etcdRunner.Stop()
	consulRunner.Stop()
}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = BeforeEach(func() {
	logger = lagertest.NewTestLogger("test")

	etcdRunner.Reset()
	consulRunner.Reset()

	etcdAdapter = etcdRunner.Adapter()
	bbs = Bbs.NewBBS(etcdAdapter, consulAdapter, clock.NewClock(), logger)

	natsPort = 4051 + GinkgoParallelNode()
	natsAddress = fmt.Sprintf("127.0.0.1:%d", natsPort)
	natsClient = diegonats.NewClient()
	natsGroupProcess = ginkgomon.Invoke(newNatsGroup())

	receptorAddress = fmt.Sprintf("127.0.0.1:%d", 6700+GinkgoParallelNode())
	receptorTaskHandlerAddress = fmt.Sprintf("127.0.0.1:%d", 1169+GinkgoParallelNode())

	receptorURL := &url.URL{
		Scheme: "http",
		Host:   receptorAddress,
		User:   url.UserPassword(username, password),
	}

	client = receptor.NewClient(receptorURL.String())

	receptorArgs = testrunner.Args{
		RegisterWithRouter: true,
		DomainNames:        "example.com",
		Address:            receptorAddress,
		TaskHandlerAddress: receptorTaskHandlerAddress,
		EtcdCluster:        etcdUrl,
		Username:           username,
		Password:           password,
		NatsAddresses:      natsAddress,
		NatsUsername:       "nats",
		NatsPassword:       "nats",
		ConsulCluster:      fmt.Sprintf("127.0.0.1:%d", consulPort+consuladapter.PortOffsetHTTP),
	}
	receptorRunner = testrunner.New(receptorBinPath, receptorArgs)
})

var _ = AfterEach(func() {
	etcdAdapter.Disconnect()
	ginkgomon.Kill(natsGroupProcess)
})

func newNatsGroup() ifrit.Runner {
	natsServerRunner = diegonats.NewGnatsdTestRunner(natsPort)
	natsClientRunner = diegonats.NewClientRunner(natsAddress, "", "", logger, natsClient)
	return grouper.NewOrdered(os.Kill, grouper.Members{
		{"natsServer", natsServerRunner},
		{"natsClient", natsClientRunner},
	})
}
