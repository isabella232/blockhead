package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"code.cloudfoundry.org/lager"
	"github.com/docker/docker/client"
	"github.com/pivotal-cf/brokerapi"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cloudfoundry-incubator/blockhead/pkg/broker"
	"github.com/cloudfoundry-incubator/blockhead/pkg/config"
	"github.com/cloudfoundry-incubator/blockhead/pkg/containermanager"
	"github.com/cloudfoundry-incubator/blockhead/pkg/containermanager/docker"
	kcm "github.com/cloudfoundry-incubator/blockhead/pkg/containermanager/kubernetes"
	"github.com/cloudfoundry-incubator/blockhead/pkg/deployer"
)

func main() {
	logger := lager.NewLogger("blockhead-broker")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))

	if len(os.Args) < 3 {
		logger.Fatal("main", errors.New("config file and/or service directory missing"))
	}

	configFilepath := os.Args[1]
	servicePath := os.Args[2]

	state, err := config.NewState(configFilepath, servicePath)
	if err != nil {
		logger.Fatal("main", err)
	}

	var manager containermanager.ContainerManager
	switch state.Config.ContainerManager {
	case "docker":
		cli, err := client.NewEnvClient()
		if err != nil {
			logger.Fatal("could not set up a docker-client", err)
		}
		manager = docker.NewDockerContainerManager(logger, cli, state.Config.ExternalAddress)
		logger.Debug("using docker containermanager")
	case "kubernetes":
		config, err := rest.InClusterConfig()
		if err != nil {
			logger.Fatal("could not set up a kubernetes-client", err)
		}
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			logger.Fatal("could not set up a kubernetes-client", err)
		}
		manager = kcm.NewKubernetesContainerManager(logger, clientset)
		logger.Debug("using kubernetes containermanager")
	default:
		logger.Fatal("no container manager in config", fmt.Errorf("no container manager specified in config %q", configFilepath))
	}

	deployer := deployer.NewEthereumDeployer(logger, state.Config.DeployerPath)
	broker := broker.NewBlockheadBroker(logger, state, manager, deployer)
	creds := brokerapi.BrokerCredentials{
		Username: state.Config.Username,
		Password: state.Config.Password,
	}
	brokerAPI := brokerapi.New(broker, logger, creds)

	log := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := fmt.Sprintf("method:%q on URL:%q", r.Method, r.URL)
			logger.Debug("start request" + info)
			h.ServeHTTP(w, r)
			logger.Debug("finished" + info)
		})
	}

	http.Handle("/", log(brokerAPI))
	logger.Debug("listening on" + string(state.Config.Port))
	logger.Fatal("http-listen", http.ListenAndServe(fmt.Sprintf(":%d", state.Config.Port), nil))
}
