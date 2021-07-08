package api

import (
	"testing"
	"time"

	"bou.ke/monkey"
	merlin "github.com/gojek/merlin/client"
	"github.com/gojek/mlp/pkg/authz/enforcer"
	"github.com/gojek/mlp/pkg/instrumentation/sentry"
	"github.com/gojek/mlp/pkg/vault"
	batchensembling "github.com/gojek/turing/api/turing/batch/ensembling"
	batchrunner "github.com/gojek/turing/api/turing/batch/runner"
	"github.com/gojek/turing/api/turing/cluster"
	"github.com/gojek/turing/api/turing/config"
	"github.com/gojek/turing/api/turing/imagebuilder"
	"github.com/gojek/turing/api/turing/middleware"
	"github.com/gojek/turing/api/turing/middleware/mocks"
	"github.com/gojek/turing/api/turing/models"
	"github.com/gojek/turing/api/turing/service"
	svcmocks "github.com/gojek/turing/api/turing/service/mocks"
	"github.com/jinzhu/gorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/xanzy/go-gitlab"
	"k8s.io/apimachinery/pkg/api/resource"
)

// MockVaultClient satisfies the vault.VaultClient interface
type MockVaultClient struct{}

func (c *MockVaultClient) GetClusterSecret(clusterName string) (*vault.ClusterSecret, error) {
	return nil, nil
}

func TestNewAppContext(t *testing.T) {
	// Create test config
	timeout, _ := time.ParseDuration("10s")
	delTimeout, _ := time.ParseDuration("1s")
	testCfg := &config.Config{
		Port: 8080,
		AuthConfig: &config.AuthorizationConfig{
			Enabled: true,
			URL:     "test-auth-url",
		},
		BatchRunnerConfig: &config.BatchRunnerConfig{
			TimeInterval: 3 * time.Minute,
		},
		DbConfig: &config.DatabaseConfig{
			Host:     "turing-db-host",
			Port:     5432,
			User:     "turing-db-user",
			Password: "turing-db-pass",
			Database: "turing-db-name",
		},
		DeployConfig: &config.DeploymentConfig{
			EnvironmentType: "dev",
			GcpProject:      "gcp-project",
			Timeout:         timeout,
			DeletionTimeout: delTimeout,
			MaxCPU:          config.Quantity(resource.MustParse("200m")),
			MaxMemory:       config.Quantity(resource.MustParse("100Mi")),
		},
		EnsemblingJobConfig: &config.EnsemblingJobConfig{
			DefaultEnvironment:             "dev",
			RecordsToProcessInOneIteration: 10,
			MaxRetryCount:                  3,
			ImageBuilderConfig: config.ImageBuilderConfig{
				Registry:             "ghcr.io",
				BaseImageRef:         "ghcr.io/gojek/turing/batch-ensembler:0.0.0-build.1-98b071d",
				BuildNamespace:       "default",
				BuildContextURI:      "git://github.com/gojek/turing.git#refs/heads/master",
				DockerfileFilePath:   "engines/batch-ensembler/app.Dockerfile",
				BuildTimeoutDuration: 10 * time.Minute,
			},
			KanikoConfig: config.KanikoConfig{
				Image:        "gcr.io/kaniko-project/executor",
				ImageVersion: "v1.5.2",
				ResourceRequestsLimits: config.ResourceRequestsLimits{
					Requests: config.Resource{
						CPU:    "500m",
						Memory: "1Gi",
					},
					Limits: config.Resource{
						CPU:    "500m",
						Memory: "1Gi",
					},
				},
			},
			DefaultConfigurations: config.DefaultEnsemblingJobConfigurations{
				BatchEnsemblingJobResources: models.BatchEnsemblingJobResources{
					DriverCPURequest:      "1",
					DriverMemoryRequest:   "1Gi",
					ExecutorReplica:       2,
					ExecutorCPURequest:    "1",
					ExecutorMemoryRequest: "1Gi",
				},
				SparkConfigAnnotations: map[string]string{
					"spark/spark.sql.execution.arrow.pyspark.enabled": "true",
				},
			},
		},
		SparkAppConfig: &config.SparkAppConfig{
			NodeSelector: map[string]string{
				"node-workload-type": "batch",
			},
			CorePerCPURequest:              1.5,
			CPURequestToCPULimit:           1.25,
			SparkVersion:                   "2.4.5",
			TolerationName:                 "batch-job",
			SubmissionFailureRetries:       3,
			SubmissionFailureRetryInterval: 10,
			FailureRetries:                 3,
			FailureRetryInterval:           10,
			PythonVersion:                  "3",
			TTLSecond:                      86400,
		},
		RouterDefaults: &config.RouterDefaults{
			Image:                   "asia.gcr.io/gcp-project-id/turing-router:1.0.0",
			FiberDebugLogEnabled:    true,
			CustomMetricsEnabled:    true,
			JaegerEnabled:           true,
			JaegerCollectorEndpoint: "jaeger-endpoint",
			LogLevel:                "INFO",
			FluentdConfig: &config.FluentdConfig{
				Image:                "image",
				Tag:                  "turing-result.log",
				FlushIntervalSeconds: 90,
			},
		},
		KubernetesLabelConfigs: &config.KubernetesLabelConfigs{
			Environment: "dev",
		},
		Sentry: sentry.Config{
			Enabled: false,
			DSN:     "",
			Labels:  nil,
		},
		VaultConfig: &config.VaultConfig{
			Address: "vault-addr",
			Token:   "vault-token",
		},
		MLPConfig: &config.MLPConfig{
			MerlinURL:        "http://mlp.example.com/api/merlin/v1",
			MLPURL:           "http://mlp.example.com/api/mlp/v1",
			MLPEncryptionKey: "key",
		},
		TuringEncryptionKey: "turing-key",
		AlertConfig: &config.AlertConfig{
			Enabled: true,
			GitLab: &config.GitlabConfig{
				Branch:     "master",
				PathPrefix: "turing",
			},
		},
		SwaggerFile: "openapi.yaml",
	}
	// Create test auth enforcer and Vault client
	me := &mocks.Enforcer{}
	me.On("UpsertPolicy", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	testEnforcer := enforcer.Enforcer(me)
	testVaultClient := &MockVaultClient{}
	// Create mock MLP Service
	mlpSvc := &svcmocks.MLPService{}
	mlpSvc.On("GetEnvironments").Return([]merlin.Environment{
		{
			Id:         1,
			Name:       "N1",
			Cluster:    "C1",
			GcpProject: "gcp-project",
		},
		{
			Id:         2,
			Name:       "N2",
			Cluster:    "C2",
			GcpProject: "gcp-project-2",
		},
	}, nil)

	// Patch the functions from other packages
	defer monkey.UnpatchAll()
	monkey.Patch(middleware.NewAuthorizer,
		func(enforcer enforcer.Enforcer) (*middleware.Authorizer, error) {
			assert.Equal(t, testEnforcer, enforcer)
			return nil, nil
		},
	)
	monkey.Patch(service.NewExperimentsService,
		func(experimentConfig map[string]interface{}) (service.ExperimentsService, error) {
			return nil, nil
		},
	)
	monkey.Patch(service.NewCryptoService,
		func(encryptionKey string) service.CryptoService {
			assert.Equal(t, testCfg.TuringEncryptionKey, encryptionKey)
			return nil
		},
	)
	monkey.Patch(service.NewMLPService,
		func(mlpBasePath string, mlpEncryptionkey string, merlinBasePath string,
		) (service.MLPService, error) {
			assert.Equal(t, testCfg.MLPConfig.MLPURL, mlpBasePath)
			assert.Equal(t, testCfg.MLPConfig.MLPEncryptionKey, mlpEncryptionkey)
			assert.Equal(t, testCfg.MLPConfig.MerlinURL, merlinBasePath)
			return mlpSvc, nil
		},
	)
	monkey.Patch(cluster.InitClusterControllers,
		func(
			cfg *config.Config,
			environmentClusterMap map[string]string,
			vaultClient vault.VaultClient,
		) (map[string]cluster.Controller, error) {
			assert.Equal(t, testCfg, cfg)
			assert.Equal(t, map[string]string{
				"N1": "C1",
			}, environmentClusterMap)
			assert.Equal(t, testVaultClient, vaultClient)
			return map[string]cluster.Controller{}, nil
		},
	)
	monkey.Patch(gitlab.NewClient,
		func(token string, options ...gitlab.ClientOptionFunc) (*gitlab.Client, error) {
			assert.Equal(t, testCfg.AlertConfig.GitLab.Token, token)
			assert.Equal(t, 1, len(options))
			return nil, nil
		},
	)
	monkey.Patch(service.NewGitlabOpsAlertService,
		func(db *gorm.DB, config config.AlertConfig) (service.AlertService, error) {
			assert.Equal(t, *testCfg.AlertConfig, config)
			return nil, nil
		},
	)
	monkey.Patch(
		middleware.NewOpenAPIValidation,
		func(file string, opt middleware.OpenAPIValidationOptions) (*middleware.OpenAPIValidation, error) {
			assert.Equal(t, testCfg.SwaggerFile, file)
			return &middleware.OpenAPIValidation{}, nil
		},
	)

	// Create expected components
	testAuthorizer, err := middleware.NewAuthorizer(testEnforcer)
	assert.NoError(t, err)
	mlpService, err := service.NewMLPService(testCfg.MLPConfig.MLPURL,
		testCfg.MLPConfig.MLPEncryptionKey, testCfg.MLPConfig.MerlinURL)
	assert.NoError(t, err)
	experimentService, err := service.NewExperimentsService(testCfg.Experiment)
	assert.NoError(t, err)

	// Validate
	appCtx, err := NewAppContext(nil, testCfg, &testEnforcer, testVaultClient)
	assert.NoError(t, err)

	alertService, err := service.NewGitlabOpsAlertService(nil, *testCfg.AlertConfig)
	assert.NoError(t, err)

	ensemblingImageBuilder, err := imagebuilder.NewEnsemberJobImageBuilder(
		nil,
		testCfg.EnsemblingJobConfig.ImageBuilderConfig,
		testCfg.EnsemblingJobConfig.KanikoConfig,
	)
	assert.Nil(t, err)

	ensemblingJobService := service.NewEnsemblingJobService(
		nil,
		testCfg.EnsemblingJobConfig.DefaultEnvironment,
		testCfg.EnsemblingJobConfig.DefaultConfigurations,
	)
	batchEnsemblingController := batchensembling.NewBatchEnsemblingController(
		nil,
		mlpSvc,
		testCfg.SparkAppConfig,
	)
	batchEnsemblingJobRunner := batchensembling.NewBatchEnsemblingJobRunner(
		batchEnsemblingController,
		ensemblingJobService,
		mlpSvc,
		ensemblingImageBuilder,
		testCfg.EnsemblingJobConfig.RecordsToProcessInOneIteration,
		testCfg.EnsemblingJobConfig.MaxRetryCount,
		testCfg.EnsemblingJobConfig.ImageBuilderConfig.BuildTimeoutDuration,
	)

	assert.Equal(t, &AppContext{
		Authorizer:            testAuthorizer,
		DeploymentService:     service.NewDeploymentService(testCfg, map[string]cluster.Controller{}),
		RoutersService:        service.NewRoutersService(nil),
		EnsemblersService:     service.NewEnsemblersService(nil),
		EnsemblingJobService:  ensemblingJobService,
		RouterVersionsService: service.NewRouterVersionsService(nil),
		EventService:          service.NewEventService(nil),
		RouterDefaults:        testCfg.RouterDefaults,
		CryptoService:         service.NewCryptoService(testCfg.TuringEncryptionKey),
		MLPService:            mlpService,
		ExperimentsService:    experimentService,
		PodLogService:         service.NewPodLogService(map[string]cluster.Controller{}),
		AlertService:          alertService,
		OpenAPIValidation:     &middleware.OpenAPIValidation{},
		BatchRunners:          []batchrunner.BatchJobRunner{batchEnsemblingJobRunner},
	}, appCtx)
}
