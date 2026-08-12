declare const durationBrand: unique symbol

export type DurationString = string & {
  [durationBrand]: never
}

function assertDurationString(value: string): asserts value is DurationString {
  if (value === "" || value[0] !== "P") {
    throw new Error(`duration ${value} must start with P`)
  }

  let index = 1
  let inTime = false
  let seenPart = false
  let totalMilliseconds = 0
  while (index < value.length) {
    if (value[index] === "T") {
      if (inTime) {
        throw new Error(`duration ${value} has duplicate time marker`)
      }
      inTime = true
      index++
      continue
    }

    const match = /^([0-9]+(?:\.[0-9]*)?|\.[0-9]+)([A-Z])/.exec(
      value.slice(index),
    )
    if (match === null) {
      throw new Error(`duration ${value} has invalid duration part`)
    }
    const amount = Number(match[1])
    const unit = match[2]
    switch (unit) {
      case "W":
        if (inTime)
          throw new Error(`duration ${value} has week unit in time section`)
        totalMilliseconds += amount * 7 * 24 * 60 * 60 * 1000
        break
      case "D":
        if (inTime)
          throw new Error(`duration ${value} has day unit in time section`)
        totalMilliseconds += amount * 24 * 60 * 60 * 1000
        break
      case "H":
        if (!inTime)
          throw new Error(
            `duration ${value} has hour unit outside time section`,
          )
        totalMilliseconds += amount * 60 * 60 * 1000
        break
      case "M":
        if (!inTime)
          throw new Error(`duration ${value} has unsupported month unit`)
        totalMilliseconds += amount * 60 * 1000
        break
      case "S":
        if (!inTime)
          throw new Error(
            `duration ${value} has second unit outside time section`,
          )
        totalMilliseconds += amount * 1000
        break
      default:
        throw new Error(`duration ${value} has unsupported unit ${unit}`)
    }
    seenPart = true
    index += match[0].length
  }

  if (!seenPart || totalMilliseconds <= 0) {
    throw new Error(`duration ${value} must be greater than zero`)
  }
}

export function durationString(value: string): DurationString {
  assertDurationString(value)
  return value
}

export interface APIConfig {
  crypto_secret: NonEmptyString
  inertia_origin: string
  private_listener: ListenerConfig
  public_artwork_base_url: string
  public_listener: ListenerConfig
  public_media_base_url: string
  sites_listener: ListenerConfig
}

export interface Config {
  api: APIConfig
  db: DBConfig
  mailer: MailerConfig
  r2: R2Config
  telemetry: TelemetryConfig
}

export interface DBConfig {
  assert_migration_schema?: boolean
  host: NonEmptyString
  max_conns: number
  name: NonEmptyString
  password: NonEmptyString
  port: number
  sslmode: SSLMode
  user: NonEmptyString
}

export type ListenerConfig =
  | TcpListenerConfig
  | UnixListenerConfig
  | SystemdListenerConfig

export type MailerConfig =
  | StdoutMailerConfig
  | TestMailerConfig
  | SesMailerConfig

export type NonEmptyString = string

export interface R2Config {
  access_key_id: NonEmptyString
  account_id: NonEmptyString
  endpoint_url?: string
  generated_sites_bucket: NonEmptyString
  images_bucket: NonEmptyString
  media_bucket: NonEmptyString
  presigned_upload_ttl: DurationString
  secret_access_key: NonEmptyString
  uploads_bucket: NonEmptyString
}

export type SSLMode =
  | "disable"
  | "allow"
  | "prefer"
  | "require"
  | "verify-ca"
  | "verify-full"

export interface SesMailerConfig {
  access_key_id: NonEmptyString
  from_email: string
  from_name: NonEmptyString
  mode: "ses"
  region: NonEmptyString
  secret_access_key: NonEmptyString
}

export interface StdoutMailerConfig {
  from_email: string
  from_name: NonEmptyString
  mode: "stdout"
}

export interface SystemdListenerConfig {
  fd_name: NonEmptyString
  kind: "systemd"
}

export interface TcpListenerConfig {
  addr: NonEmptyString
  kind: "tcp"
}

export interface TelemetryConfig {
  otlp_endpoint?: NonEmptyString
  otlp_headers?: NonEmptyString
  otlp_traces_endpoint?: NonEmptyString
  otlp_traces_headers?: NonEmptyString
}

export interface TestMailerConfig {
  from_email: string
  from_name: NonEmptyString
  mode: "test"
}

export interface UnixListenerConfig {
  kind: "unix"
  path: NonEmptyString
}
