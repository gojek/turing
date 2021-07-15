package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gojek/mlp/pkg/instrumentation/newrelic"
	"github.com/gojek/mlp/pkg/instrumentation/sentry"
	"github.com/gojek/turing/api/turing/models"
	"github.com/mitchellh/mapstructure"

	// Using a maintained fork of https://github.com/spf13/viper mainly so that viper.AllSettings()
	// always returns map[string]interface{}. Without this, config for experiment cannot be
	// easily marshalled into JSON, which is currently the format required for experiment config.
	"github.com/ory/viper"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Quantity is an alias for resource.Quantity
type Quantity resource.Quantity

// Decode parses the Quantity config and checks that it is non-empty and can be converted into
// a valid resource.Quantity.
func (qty *Quantity) Decode(value string) (err error) {
	if value == "" {
		return errors.New("value is empty")
	}

	// MustParse panics if the supplied value cannot be parsed
	defer func() {
		if panicErr := recover(); panicErr != nil {
			err = panicErr.(error)
		}
	}()
	*qty = Quantity(resource.MustParse(value))

	return nil
}

// MarshalJSON implements the json.Marshaller interface so that we will get the expected
// JSON string representation when marshalling a quantity object.
func (qty *Quantity) MarshalJSON() ([]byte, error) {
	q := resource.Quantity(*qty)
	return q.MarshalJSON()
}

// Config is used to parse and store the environment configs
type Config struct {
	Port                   int `validate:"required"`
	AllowedOrigins         []string
	AuthConfig             *AuthorizationConfig
	BatchRunnerConfig      *BatchRunnerConfig      `validate:"required"`
	DbConfig               *DatabaseConfig         `validate:"required"`
	DeployConfig           *DeploymentConfig       `validate:"required"`
	EnsemblingJobConfig    *EnsemblingJobConfig    `validate:"required"`
	SparkAppConfig         *SparkAppConfig         `validate:"required"`
	RouterDefaults         *RouterDefaults         `validate:"required"`
	KubernetesLabelConfigs *KubernetesLabelConfigs `validate:"required"`
	NewRelicConfig         newrelic.Config
	Sentry                 sentry.Config
	VaultConfig            *VaultConfig `validate:"required"`
	TuringEncryptionKey    string       `validate:"required"`
	AlertConfig            *AlertConfig
	MLPConfig              *MLPConfig `validate:"required"`
	TuringUIConfig         *TuringUIConfig
	// SwaggerFile specifies the file path containing OpenAPI v3 spec. This file will be used to configure
	// OpenAPI validation middleware, which validates HTTP requests against the spec.
	SwaggerFile string
	// Experiment specifies the JSON configuration to set up experiment managers and runners.
	//
	// The configuration follows the following format to support different experiment engines
	// and to allow custom configuration for different engines:
	// { "<experiment_engine>": <experiment_engine_config>, ... }
	//
	// For example:
	// { "experiment_engine_a": {"client": "foo"}, "experiment_engine_b": {"apikey": 12} }
	Experiment map[string]interface{}
}

// ListenAddress returns the Turing Api app's port
func (c *Config) ListenAddress() string {
	return fmt.Sprintf(":%d", c.Port)
}

func (c *Config) Validate() error {
	validate, err := newConfigValidator()
	if err != nil {
		return err
	}
	return validate.Struct(c)
}

// BatchRunnerConfig captures the config related to the running of batch runners
type BatchRunnerConfig struct {
	TimeInterval time.Duration `validate:"required"`
}

// DeploymentConfig captures the config related to the deployment of the turing routers
type DeploymentConfig struct {
	EnvironmentType string `validate:"required"`
	GcpProject      string
	Timeout         time.Duration `validate:"required"`
	DeletionTimeout time.Duration `validate:"required"`
	MaxCPU          Quantity      `validate:"required"`
	MaxMemory       Quantity      `validate:"required"`
}

// KubernetesLabelConfigs are the configurations for labeling
type KubernetesLabelConfigs struct {
	// LabelPrefix is the prefix used for tagging kubernetes components.
	// Default is an empty string which means your tags will look something like this:
	//   team: teen-titans
	//   stream: nile
	//   environment: dev
	//   orchestrator: turing
	//   app: my-model-app
	LabelPrefix string
	// Environment is the value for the environment label
	Environment string `validate:"required"`
}

// EnsemblingJobConfig captures the config related to the ensembling batch jobs.
type EnsemblingJobConfig struct {
	// RecordsToProcessInOneIteration dictates the number of batch ensembling jobs to be queried at once.
	RecordsToProcessInOneIteration int `validate:"required"`
	// MaxRetryCount is the number of retries the batch ensembler runner should try before giving up.
	MaxRetryCount int `validate:"required"`
	// DefaultEnvironment is the environment used for image building and running the batch ensemblers.
	DefaultEnvironment string `validate:"required"`
	// KanikoConfig contains the configuration related to the kaniko executor image
	KanikoConfig KanikoConfig `validate:"required"`
	// ImageBuilderConfig contains the configuration related to the built ensembler image itself.
	ImageBuilderConfig ImageBuilderConfig `validate:"required"`
	// DefaultConfigurations contains the default configurations applied to the ensembling job.
	// The user (the person who calls the API) is free to override/append the default values.
	DefaultConfigurations DefaultEnsemblingJobConfigurations `validate:"required"`
}

// DefaultEnsemblingJobConfigurations contains the default configurations applied to the ensembling job.
type DefaultEnsemblingJobConfigurations struct {
	// BatchEnsemblingJobResources contains the resources delared to run the ensembling job.
	BatchEnsemblingJobResources models.BatchEnsemblingJobResources
	// SparkConfigAnnotations contains the Spark configurations
	SparkConfigAnnotations map[string]string
}

// ImageBuilderConfig provides the configuration used for the OCI image building.
// The details here contain the details pertaining to the ensembler image and not the kaniko image.
type ImageBuilderConfig struct {
	// Registry is the registry of the newly built ensembler image.
	Registry string `validate:"required"`
	// BaseImageRef is the image name of the base ensembler image based on engines/batch-ensembler/Dockerfile.
	BaseImageRef string `validate:"required"`
	// BuildNamespace contains the Kubernetes namespace it should be built in.
	BuildNamespace string `validate:"required"`
	// BuildContextURI contains the image build context, which should be engines/batch-ensembler/
	// The forms supported are listed here https://github.com/GoogleContainerTools/kaniko#kaniko-build-contexts
	BuildContextURI string `validate:"required"`
	// DockerfileFilePath contains where the Dockerfile is
	DockerfileFilePath string `validate:"required"`
	// BuildTimeoutDuration is the Kubernetes Job timeout duration.
	BuildTimeoutDuration time.Duration
}

// Resource contains the Kubernetes resource request and limits
type Resource struct {
	CPU    string `validate:"required"`
	Memory string `validate:"required"`
}

// ResourceRequestsLimits contains the Kubernetes resource request and limits for kaniko
type ResourceRequestsLimits struct {
	Requests Resource `validate:"required"`
	Limits   Resource `validate:"required"`
}

// KanikoConfig provides the configuration used for the Kaniko image.
type KanikoConfig struct {
	Image                  string                 `validate:"required"`
	ImageVersion           string                 `validate:"required"`
	ResourceRequestsLimits ResourceRequestsLimits `validate:"required"`
}

// SparkAppConfig contains the infra configurations that is unique to the user's Kubernetes
type SparkAppConfig struct {
	NodeSelector                   map[string]string
	CorePerCPURequest              float64 `validate:"required"`
	CPURequestToCPULimit           float64 `validate:"required"`
	SparkVersion                   string  `validate:"required"`
	TolerationName                 string  `validate:"required"`
	SubmissionFailureRetries       int32   `validate:"required"`
	SubmissionFailureRetryInterval int64   `validate:"required"`
	FailureRetries                 int32   `validate:"required"`
	FailureRetryInterval           int64   `validate:"required"`
	PythonVersion                  string  `validate:"required"`
	TTLSecond                      int64   `validate:"required"`
	TaintKey                       *string
}

// TuringUIConfig captures config related to serving Turing UI files
type TuringUIConfig struct {
	// Optional. If configured, turing-api will serve static files of the turing-ui React app
	AppDirectory string
	// Optional. Defines the relative path under which the app will be accessible.
	// This should match `homepage` value from the `package.json` file of the CRA app
	Homepage string
}

// DatabaseConfig config captures the Turing database config
type DatabaseConfig struct {
	Host     string `validate:"required"`
	Port     int    `validate:"required"`
	User     string `validate:"required"`
	Password string `validate:"required"`
	Database string `validate:"required"`
}

// RouterDefaults contains default configuration for routers deployed
// by this isntance of the Turing API.
type RouterDefaults struct {
	// Turing router image, in the format registry/repository:version.
	Image string `validate:"required"`
	// Enable Fiber debug logging
	FiberDebugLogEnabled bool
	// Enable router custom metrics
	CustomMetricsEnabled bool
	// Enable Jaeger Tracing
	JaegerEnabled bool
	// Jaeger collector endpoint. If JaegerEnabled is true, this value
	// must be set.
	JaegerCollectorEndpoint string
	// Router log level
	LogLevel string `validate:"required"`
	// Fluentd config for the router
	FluentdConfig *FluentdConfig
	// Experiment specifies the default experiment JSON configuration for different experiment
	// engines that Turing router supports.
	//
	// The JSON configuration follows the following format to support different experiment engines.
	// { "<experiment_engine>": <experiment_engine_config>, ... }
	//
	// Currently the router defaults configuration for each experiment engine is expected to have
	// "endpoint" and "timeout" fields with string value, in order to follow the configuration
	// specification for routers.
	//
	// For example:
	// {"experiment_engine_a": {"endpoint": "http://engine-a.com", "timeout": "500ms"},
	//  "experiment_engine_b": {"endpoint": "http://engine-b.com", "timeout": "250ms"} }
	Experiment map[string]interface{}
}

// FluentdConfig captures the defaults used by the Turing Router when Fluentd is enabled
type FluentdConfig struct {
	// Image to use for fluentd deployments, in the format registry/repository:version.
	Image string
	// Fluentd tag for logs
	Tag string
	// Flush interval seconds - value determined by load job frequency to BQ
	FlushIntervalSeconds int
}

// AuthorizationConfig captures the config for auth using mlp authz
type AuthorizationConfig struct {
	Enabled bool
	URL     string
}

// VaultConfig captures the config for connecting to the Vault server
type VaultConfig struct {
	Address string `validate:"required"`
	Token   string `validate:"required"`
}

type AlertConfig struct {
	Enabled bool
	GitLab  *GitlabConfig
	// PlaybookURL is the URL that contains documentation on how to resolve triggered alerts
	PlaybookURL string
	// DashboardURLTemplate is a template for grafana dashboard URL that shows router metrics.
	// The template accepts go-template format and will be executed with dashboardURLValue which has
	// the following fields: Environment, Cluster, Project, Router, Version.
	DashboardURLTemplate string
}

type GitlabConfig struct {
	BaseURL    string
	Token      string
	ProjectID  string
	Branch     string
	PathPrefix string
}

// MLPConfig captures the configuration used to connect to the Merlin/MLP API servers
type MLPConfig struct {
	MerlinURL        string `validate:"required"`
	MLPURL           string `validate:"required"`
	MLPEncryptionKey string `validate:"required"`
}

// Load creates a Config object from default config values, config files and environment variables.
// Load accepts config files as the argument. JSON and YAML format are both supported.
//
// If multiple config files are provided, the subsequent config files will override the config
// values from the config files loaded earlier.
//
// These config files will override the default config values (refer to setDefaultValues function)
// and can be overridden by the values from environment variables. Nested keys in the config
// can be set from environment variable name separed by "_". For instance the config value for
// "DbConfig.Port" can be overridden by environment variable name "DBCONFIG_PORT". Note that
// all environment variable names must be upper case.
//
// If no config file is provided, only the default config values and config values from environment
// varibales will be loaded.
//
// Refer to example.yaml for an example of config file.
func Load(filepaths ...string) (*Config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))

	// Load default config values
	setDefaultValues(v)

	// Load config values from the provided config files
	for _, f := range filepaths {
		v.SetConfigFile(f)
		err := v.MergeInConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to read config from file '%s': %s", f, err)
		}
	}

	// Load config values from environment variables.
	// Nested keys in the config is represented by variable name separated by '_'.
	// For example, DbConfig.Host can be set from environment variable DBCONFIG_HOST.
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()

	config := &Config{}

	// Unmarshal config values into the config object.
	// Add StringToQuantityHookFunc() to the default DecodeHook in order to parse quantity string
	// into quantity object. Refs:
	// https://github.com/spf13/viper/blob/493643fd5e4b44796124c05d59ee04ba5f809e19/viper.go#L1003-L1005
	// https://github.com/mitchellh/mapstructure/blob/9e1e4717f8567d7ead72d070d064ad17d444a67e/decode_hooks_test.go#L128
	err := v.Unmarshal(config, func(c *mapstructure.DecoderConfig) {
		c.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			StringToQuantityHookFunc(),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config values: %s", err)
	}

	return config, nil
}

// setDefaultValues for all keys in Viper config. We need to set values for all keys so that
// we can always use environment variables to override the config keys. In Viper v1, if the
// keys do not have default values, and the key does not appear in the config file, it cannot
// be overridden by environment variables, unless each key is called with BindEnv.
// https://github.com/spf13/viper/issues/188
// https://github.com/spf13/viper/issues/761
func setDefaultValues(v *viper.Viper) {
	v.SetDefault("Port", "8080")

	v.SetDefault("AllowedOrigins", "*")

	v.SetDefault("AuthConfig::Enabled", "false")
	v.SetDefault("AuthConfig::URL", "")

	v.SetDefault("DbConfig::Host", "localhost")
	v.SetDefault("DbConfig::Port", "5432")
	v.SetDefault("DbConfig::User", "")
	v.SetDefault("DbConfig::Password", "")
	v.SetDefault("DbConfig::Database", "turing")

	v.SetDefault("DeployConfig::EnvironmentType", "")
	v.SetDefault("DeployConfig::GcpProject", "")
	v.SetDefault("DeployConfig::Timeout", "3m")
	v.SetDefault("DeployConfig::DeletionTimeout", "1m")
	v.SetDefault("DeployConfig::MaxCPU", "4")
	v.SetDefault("DeployConfig::MaxMemory", "8Gi")

	v.SetDefault("RouterDefaults::Image", "")
	v.SetDefault("RouterDefaults::FiberDebugLogEnabled", "false")
	v.SetDefault("RouterDefaults::CustomMetricsEnabled", "false")
	v.SetDefault("RouterDefaults::JaegerEnabled", "false")
	v.SetDefault("RouterDefaults::JaegerCollectorEndpoint", "")
	v.SetDefault("RouterDefaults::LogLevel", "INFO")
	v.SetDefault("RouterDefaults::FluentdConfig::Image", "")
	v.SetDefault("RouterDefaults::FluentdConfig::Tag", "turing-result.log")
	v.SetDefault("RouterDefaults::FluentdConfig::FlushIntervalSeconds", "90")
	v.SetDefault("RouterDefaults::Experiment", map[string]interface{}{})

	v.SetDefault("Sentry::Enabled", "false")
	v.SetDefault("Sentry::DSN", "")

	v.SetDefault("VaultConfig::Address", "http://localhost:8200")
	v.SetDefault("VaultConfig::Token", "")

	v.SetDefault("TuringEncryptionKey", "")

	v.SetDefault("AlertConfig::Enabled", "false")
	v.SetDefault("AlertConfig::GitLab::BaseURL", "https://gitlab.com")
	v.SetDefault("AlertConfig::GitLab::Token", "")
	v.SetDefault("AlertConfig::GitLab::ProjectID", "")
	v.SetDefault("AlertConfig::GitLab::Branch", "master")
	v.SetDefault("AlertConfig::GitLab::PathPrefix", "turing")

	v.SetDefault("MLPConfig::MerlinURL", "")
	v.SetDefault("MLPConfig::MLPURL", "")
	v.SetDefault("MLPConfig::MLPEncryptionKey", "")

	v.SetDefault("TuringUIConfig::AppDirectory", "")
	v.SetDefault("TuringUIConfig::Homepage", "/turing")

	v.SetDefault("SwaggerFile", "openapi.yaml")
	v.SetDefault("Experiment", map[string]interface{}{})
}

func newConfigValidator() (*validator.Validate, error) {
	v := validator.New()
	// Use struct level validation for AuthorizationConfig
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		field := sl.Current().Interface().(AuthorizationConfig)
		// If auth is enabled, URL should be set
		if field.Enabled && field.URL == "" {
			sl.ReportError(field.URL, "authorization_url", "URL", "url-set", "")
		}
	}, AuthorizationConfig{})
	return v, nil
}

// StringToQuantityHookFunc converts string to quantity type. This function is required since
// viper uses mapstructure to unmarshal values.
// https://github.com/spf13/viper#unmarshaling
// https://pkg.go.dev/github.com/mitchellh/mapstructure#DecodeHookFunc
func StringToQuantityHookFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}

		if t != reflect.TypeOf(Quantity{}) {
			return data, nil
		}

		// Convert it by parsing
		q, err := resource.ParseQuantity(data.(string))
		if err != nil {
			return nil, err
		}

		return Quantity(q), nil
	}
}
