package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type APIConfig struct {
	CryptoSecret         NonEmptyString `json:"crypto_secret"`
	InertiaOrigin        string         `json:"inertia_origin"`
	PrivateListener      ListenerConfig `json:"private_listener"`
	PublicArtworkBaseUrl string         `json:"public_artwork_base_url"`
	PublicListener       ListenerConfig `json:"public_listener"`
	PublicMediaBaseUrl   string         `json:"public_media_base_url"`
	SitesListener        ListenerConfig `json:"sites_listener"`
}

type Config struct {
	Api       APIConfig       `json:"api"`
	Db        DBConfig        `json:"db"`
	Mailer    MailerConfig    `json:"mailer"`
	R2        R2Config        `json:"r2"`
	Telemetry TelemetryConfig `json:"telemetry"`
}

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

// ListenerConfig is generated from an OpenAPI oneOf schema.
// oneOf variant: TcpListenerConfig
// oneOf variant: UnixListenerConfig
// oneOf variant: SystemdListenerConfig
type ListenerConfig struct {
	TcpListenerConfig     *TcpListenerConfig
	UnixListenerConfig    *UnixListenerConfig
	SystemdListenerConfig *SystemdListenerConfig
}

func (dst *ListenerConfig) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Value string `json:"kind"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode ListenerConfig discriminator: %w", err)
	}
	switch discriminator.Value {
	case "systemd":
		var decoded SystemdListenerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode ListenerConfig as SystemdListenerConfig: %w", err)
		}
		*dst = ListenerConfig{SystemdListenerConfig: &decoded}
		return nil
	case "tcp":
		var decoded TcpListenerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode ListenerConfig as TcpListenerConfig: %w", err)
		}
		*dst = ListenerConfig{TcpListenerConfig: &decoded}
		return nil
	case "unix":
		var decoded UnixListenerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode ListenerConfig as UnixListenerConfig: %w", err)
		}
		*dst = ListenerConfig{UnixListenerConfig: &decoded}
		return nil
	default:
		return fmt.Errorf("unsupported ListenerConfig discriminator %q", discriminator.Value)
	}
}

func (src ListenerConfig) MarshalJSON() ([]byte, error) {
	matchCount := 0
	if src.TcpListenerConfig != nil {
		matchCount++
	}
	if src.UnixListenerConfig != nil {
		matchCount++
	}
	if src.SystemdListenerConfig != nil {
		matchCount++
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("ListenerConfig must contain exactly one variant, got %d", matchCount)
	}
	if src.TcpListenerConfig != nil {
		return json.Marshal(src.TcpListenerConfig)
	}
	if src.UnixListenerConfig != nil {
		return json.Marshal(src.UnixListenerConfig)
	}
	if src.SystemdListenerConfig != nil {
		return json.Marshal(src.SystemdListenerConfig)
	}
	return nil, fmt.Errorf("ListenerConfig has no variant")
}

func (src ListenerConfig) GetActualInstance() any {
	if src.TcpListenerConfig != nil {
		return src.TcpListenerConfig
	}
	if src.UnixListenerConfig != nil {
		return src.UnixListenerConfig
	}
	if src.SystemdListenerConfig != nil {
		return src.SystemdListenerConfig
	}
	return nil
}

func TcpListenerConfigAsListenerConfig(v TcpListenerConfig) ListenerConfig {
	return ListenerConfig{TcpListenerConfig: &v}
}

func UnixListenerConfigAsListenerConfig(v UnixListenerConfig) ListenerConfig {
	return ListenerConfig{UnixListenerConfig: &v}
}

func SystemdListenerConfigAsListenerConfig(v SystemdListenerConfig) ListenerConfig {
	return ListenerConfig{SystemdListenerConfig: &v}
}

// MailerConfig is generated from an OpenAPI oneOf schema.
// oneOf variant: StdoutMailerConfig
// oneOf variant: TestMailerConfig
// oneOf variant: SesMailerConfig
type MailerConfig struct {
	StdoutMailerConfig *StdoutMailerConfig
	TestMailerConfig   *TestMailerConfig
	SesMailerConfig    *SesMailerConfig
}

func (dst *MailerConfig) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Value string `json:"mode"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode MailerConfig discriminator: %w", err)
	}
	switch discriminator.Value {
	case "ses":
		var decoded SesMailerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode MailerConfig as SesMailerConfig: %w", err)
		}
		*dst = MailerConfig{SesMailerConfig: &decoded}
		return nil
	case "stdout":
		var decoded StdoutMailerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode MailerConfig as StdoutMailerConfig: %w", err)
		}
		*dst = MailerConfig{StdoutMailerConfig: &decoded}
		return nil
	case "test":
		var decoded TestMailerConfig
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode MailerConfig as TestMailerConfig: %w", err)
		}
		*dst = MailerConfig{TestMailerConfig: &decoded}
		return nil
	default:
		return fmt.Errorf("unsupported MailerConfig discriminator %q", discriminator.Value)
	}
}

func (src MailerConfig) MarshalJSON() ([]byte, error) {
	matchCount := 0
	if src.StdoutMailerConfig != nil {
		matchCount++
	}
	if src.TestMailerConfig != nil {
		matchCount++
	}
	if src.SesMailerConfig != nil {
		matchCount++
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("MailerConfig must contain exactly one variant, got %d", matchCount)
	}
	if src.StdoutMailerConfig != nil {
		return json.Marshal(src.StdoutMailerConfig)
	}
	if src.TestMailerConfig != nil {
		return json.Marshal(src.TestMailerConfig)
	}
	if src.SesMailerConfig != nil {
		return json.Marshal(src.SesMailerConfig)
	}
	return nil, fmt.Errorf("MailerConfig has no variant")
}

func (src MailerConfig) GetActualInstance() any {
	if src.StdoutMailerConfig != nil {
		return src.StdoutMailerConfig
	}
	if src.TestMailerConfig != nil {
		return src.TestMailerConfig
	}
	if src.SesMailerConfig != nil {
		return src.SesMailerConfig
	}
	return nil
}

func StdoutMailerConfigAsMailerConfig(v StdoutMailerConfig) MailerConfig {
	return MailerConfig{StdoutMailerConfig: &v}
}

func TestMailerConfigAsMailerConfig(v TestMailerConfig) MailerConfig {
	return MailerConfig{TestMailerConfig: &v}
}

func SesMailerConfigAsMailerConfig(v SesMailerConfig) MailerConfig {
	return MailerConfig{SesMailerConfig: &v}
}

type NonEmptyString = string

type R2Config struct {
	AccessKeyId          NonEmptyString `json:"access_key_id"`
	AccountId            NonEmptyString `json:"account_id"`
	EndpointUrl          *string        `json:"endpoint_url,omitempty"`
	GeneratedSitesBucket NonEmptyString `json:"generated_sites_bucket"`
	ImagesBucket         NonEmptyString `json:"images_bucket"`
	MediaBucket          NonEmptyString `json:"media_bucket"`
	PresignedUploadTtl   ISODuration    `json:"presigned_upload_ttl"`
	SecretAccessKey      NonEmptyString `json:"secret_access_key"`
	UploadsBucket        NonEmptyString `json:"uploads_bucket"`
}

type SSLMode string

const (
	SSLModeDisable    SSLMode = "disable"
	SSLModeAllow      SSLMode = "allow"
	SSLModePrefer     SSLMode = "prefer"
	SSLModeRequire    SSLMode = "require"
	SSLModeVerifyCa   SSLMode = "verify-ca"
	SSLModeVerifyFull SSLMode = "verify-full"
)

type SesMailerConfig struct {
	AccessKeyId     NonEmptyString `json:"access_key_id"`
	FromEmail       string         `json:"from_email"`
	FromName        NonEmptyString `json:"from_name"`
	Mode            string         `json:"mode"`
	Region          NonEmptyString `json:"region"`
	SecretAccessKey NonEmptyString `json:"secret_access_key"`
}

type StdoutMailerConfig struct {
	FromEmail string         `json:"from_email"`
	FromName  NonEmptyString `json:"from_name"`
	Mode      string         `json:"mode"`
}

type SystemdListenerConfig struct {
	FdName NonEmptyString `json:"fd_name"`
	Kind   string         `json:"kind"`
}

type TcpListenerConfig struct {
	Addr NonEmptyString `json:"addr"`
	Kind string         `json:"kind"`
}

type TelemetryConfig struct {
	OtlpEndpoint       *NonEmptyString `json:"otlp_endpoint,omitempty"`
	OtlpHeaders        *NonEmptyString `json:"otlp_headers,omitempty"`
	OtlpTracesEndpoint *NonEmptyString `json:"otlp_traces_endpoint,omitempty"`
	OtlpTracesHeaders  *NonEmptyString `json:"otlp_traces_headers,omitempty"`
}

type TestMailerConfig struct {
	FromEmail string         `json:"from_email"`
	FromName  NonEmptyString `json:"from_name"`
	Mode      string         `json:"mode"`
}

type UnixListenerConfig struct {
	Kind string         `json:"kind"`
	Path NonEmptyString `json:"path"`
}

type ISODuration time.Duration

func (dst *ISODuration) UnmarshalJSON(data []byte) error {
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode ISO duration: %w", err)
	}
	parsed, err := parseISODuration(decoded)
	if err != nil {
		return err
	}
	*dst = ISODuration(parsed)
	return nil
}

func (src ISODuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(formatISODuration(src))
}

const (
	dayDuration  = 24 * time.Hour
	weekDuration = 7 * dayDuration
)

func formatISODuration(value ISODuration) string {
	return "PT" + strconv.FormatFloat(float64(value)/float64(time.Second), 'f', -1, 64) + "S"
}

func parseISODuration(value string) (time.Duration, error) {
	if value == "" || value[0] != 'P' {
		return 0, fmt.Errorf("duration %q must start with P", value)
	}

	var total time.Duration
	inTime := false
	seenPart := false
	for index := 1; index < len(value); {
		if value[index] == 'T' {
			if inTime {
				return 0, fmt.Errorf("duration %q has duplicate time marker", value)
			}
			inTime = true
			index++
			continue
		}

		nextIndex, amount, unit, err := scanDurationPart(value, index)
		if err != nil {
			return 0, err
		}
		durationPart, err := fixedISODurationPart(value, unit, amount, inTime)
		if err != nil {
			return 0, err
		}
		total += durationPart
		seenPart = true
		index = nextIndex
	}
	if !seenPart {
		return 0, fmt.Errorf("duration %q must contain at least one duration part", value)
	}
	return total, nil
}

func scanDurationPart(value string, index int) (int, float64, byte, error) {
	start := index
	for index < len(value) && isDurationNumberByte(value[index]) {
		index++
	}
	if start == index || index >= len(value) {
		return 0, 0, 0, fmt.Errorf("duration %q has invalid duration part", value)
	}
	amount, err := strconv.ParseFloat(value[start:index], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("duration %q has invalid numeric value %q: %w", value, value[start:index], err)
	}
	return index + 1, amount, value[index], nil
}

func isDurationNumberByte(value byte) bool {
	return (value >= '0' && value <= '9') || value == '.'
}

func fixedISODurationPart(
	value string,
	unit byte,
	amount float64,
	inTime bool,
) (time.Duration, error) {
	switch unit {
	case 'W':
		return fixedISODateDurationPart(value, unit, amount, weekDuration, inTime)
	case 'D':
		return fixedISODateDurationPart(value, unit, amount, dayDuration, inTime)
	case 'H':
		return fixedISOTimeDurationPart(value, unit, amount, time.Hour, inTime)
	case 'M':
		if !inTime {
			return 0, fmt.Errorf("duration %q has unsupported month unit", value)
		}
		return time.Duration(amount * float64(time.Minute)), nil
	case 'S':
		return fixedISOTimeDurationPart(value, unit, amount, time.Second, inTime)
	default:
		return 0, fmt.Errorf("duration %q has unsupported unit %q", value, string(unit))
	}
}

func fixedISODateDurationPart(
	value string,
	unit byte,
	amount float64,
	scale time.Duration,
	inTime bool,
) (time.Duration, error) {
	if inTime {
		return 0, fmt.Errorf("duration %q has %s unit in time section", value, isoDurationUnitName(unit))
	}
	return time.Duration(amount * float64(scale)), nil
}

func fixedISOTimeDurationPart(
	value string,
	unit byte,
	amount float64,
	scale time.Duration,
	inTime bool,
) (time.Duration, error) {
	if !inTime {
		return 0, fmt.Errorf("duration %q has %s unit outside time section", value, isoDurationUnitName(unit))
	}
	return time.Duration(amount * float64(scale)), nil
}

func isoDurationUnitName(unit byte) string {
	switch unit {
	case 'W':
		return "week"
	case 'D':
		return "day"
	case 'H':
		return "hour"
	case 'S':
		return "second"
	default:
		return string(unit)
	}
}
