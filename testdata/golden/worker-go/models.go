package workerconfig

type DBConfig struct {
	AssertMigrationSchema *bool          `json:"assert_migration_schema,omitempty"`
	Host                  NonEmptyString `json:"host"`
	MaxConns              int32          `json:"max_conns"`
	Name                  NonEmptyString `json:"name"`
	Password              NonEmptyString `json:"password"`
	Port                  int32          `json:"port"`
	Sslmode               SSLMode        `json:"sslmode"`
	User                  NonEmptyString `json:"user"`
}

type NonEmptyString = string

type SSLMode string

const (
	SSLModeDisable    SSLMode = "disable"
	SSLModeAllow      SSLMode = "allow"
	SSLModePrefer     SSLMode = "prefer"
	SSLModeRequire    SSLMode = "require"
	SSLModeVerifyCa   SSLMode = "verify-ca"
	SSLModeVerifyFull SSLMode = "verify-full"
)

type WorkerConfig struct {
	Db     DBConfig      `json:"db"`
	Queues []WorkerQueue `json:"queues"`
}

type WorkerQueue string

const (
	WorkerQueueCompute WorkerQueue = "compute"
)
