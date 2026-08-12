export type ApiKeyScope =
  | "show:create"
  | "show:update"
  | "episode:create"
  | "episode:update"
  | "episode:publish"

export type ApiKeySecretHash = string

export interface AppShellData {
  teams: readonly Team[]
  user: User
}

export interface CompleteMediaMultipartUpload {
  parts: readonly CompletedMediaUploadPart[]
}

export interface CompletedMediaMultipartUpload {
  byte_length: number
  completed_at: UnixMillis
  content_type: NonEmptyString
  object_key: MediaUploadObjectKey
  processing: EpisodeProcessingSnapshot
  upload_session_id: UploadSessionID
}

export interface CompletedMediaUploadPart {
  etag: NonEmptyString
  part_number: MediaUploadPartNumber
}

export interface CreateAPIKey {
  name: NonEmptyString
  scopes: readonly ApiKeyScope[]
}

export interface CreateEpisode {
  description?: string
  enclosures?: readonly EpisodeEnclosure[]
  republished_at?: UnixMillis
  slug?: NonEmptyString
  status?: EpisodeStatus
  thumbnail_url?: string
  title?: NonEmptyString
}

export interface CreateImageUploadPresign {
  byte_length: number
  content_type: NonEmptyString
  file_name?: NonEmptyString
}

export interface CreateMediaMultipartUpload {
  byte_length: number
  content_type: NonEmptyString
  episode_id?: EpisodeID
  file_name?: NonEmptyString
  show_id?: ShowID
}

export interface CreateMediaUploadPresign {
  byte_length: number
  content_type: NonEmptyString
  episode_id?: EpisodeID
  file_name?: NonEmptyString
  show_id?: ShowID
}

export interface CreateShow {
  description?: string
  slug: NonEmptyString
  source_kind: ShowSourceKind
  title: NonEmptyString
  website_url?: string
}

export interface CreateTeam {
  name: NonEmptyString
}

export interface CreatedAPIKey {
  key: string
  scopes: readonly ApiKeyScope[]
  secret_hash: ApiKeySecretHash
}

export interface CreatedImageUploadPresign {
  content_type: NonEmptyString
  expires_at: UnixMillis
  method: "PUT"
  object_key: ImageUploadObjectKey
  public_url: string
  upload_url: string
}

export interface CreatedMediaMultipartUpload {
  byte_length: number
  content_type: NonEmptyString
  expires_at: UnixMillis
  max_parallelism: number
  object_key: MediaUploadObjectKey
  part_size: number
  upload_session_id: UploadSessionID
}

export interface CreatedMediaUploadPresign {
  content_type: NonEmptyString
  expires_at: UnixMillis
  method: "PUT"
  object_key: MediaUploadObjectKey
  upload_url: string
}

export interface EmailVerification {
  token: NonEmptyString
}

export interface Episode {
  description?: string
  enclosures: readonly EpisodeEnclosure[]
  id: EpisodeID
  processing?: EpisodeProcessingSnapshot
  republished_at?: UnixMillis
  show_id: ShowID
  slug: NonEmptyString
  status: EpisodeStatus
  thumbnail_url?: string
  title: NonEmptyString
}

export interface EpisodeEnclosure {
  byte_length: number
  content_type: NonEmptyString
  duration_seconds: number
  feed_id: FeedID
  format: EpisodeEnclosureFormat
  url: string
}

export type EpisodeEnclosureFormat = "mp3" | "m4a" | "mp4"

export type EpisodeID = string

export interface EpisodeProcessingCompleteEvent {
  current_step: NonEmptyString
  error_code?: NonEmptyString
  error_message?: NonEmptyString
  percent: number
  run_id: NonEmptyString
  status: EpisodeProcessingStatus
  type: "episode.processing.complete"
}

export interface EpisodeProcessingSnapshot {
  current_step: NonEmptyString
  error_code?: NonEmptyString
  error_message?: NonEmptyString
  percent: number
  run_id: NonEmptyString
  status: EpisodeProcessingStatus
}

export type EpisodeProcessingStatus =
  | "queued"
  | "processing"
  | "completed"
  | "failed"

export type EpisodeStatus = "draft" | "published"

export type FeedID = string

export interface GoogleSignup {
  email: string
  email_verified: boolean
  google_sub: NonEmptyString
  name?: NonEmptyString
  picture_url?: string
}

export type ImageUploadObjectKey = string

export interface ListedAPIKey {
  created_at: UnixMillis
  name: NonEmptyString
  scopes: readonly ApiKeyScope[]
  secret_hash: ApiKeySecretHash
  secret_suffix: NonEmptyString
}

export type MediaUploadObjectKey = string

export type MediaUploadPartNumber = number

export type NonEmptyString = string

export interface PasswordAuth {
  email: string
  password: NonEmptyString
}

export interface PasswordResetConfirmation {
  password: NonEmptyString
  token: NonEmptyString
}

export interface PasswordResetRequest {
  email: string
}

export interface PresignMediaUploadParts {
  part_numbers: readonly MediaUploadPartNumber[]
}

export interface PresignedMediaUploadPart {
  expires_at: UnixMillis
  method: "PUT"
  part_number: MediaUploadPartNumber
  upload_url: string
}

export interface PresignedMediaUploadParts {
  parts: readonly PresignedMediaUploadPart[]
  upload_session_id: UploadSessionID
}

export interface Session {
  expires_at: UnixMillis
  id: SessionID
  user_id: UserID
}

export type SessionID = string

export interface Show {
  description?: string
  id: ShowID
  image_url?: string
  slug: NonEmptyString
  source_kind: ShowSourceKind
  team_id: TeamID
  title: NonEmptyString
  website_url?: string
}

export type ShowID = string

export interface ShowPage {
  episodes: readonly Episode[]
  shell: AppShellData
  show: Show
}

export type ShowSourceKind = "audio" | "video"

export interface ShowsPage {
  shell: AppShellData
  shows: readonly Show[]
  team_id: TeamID
}

export interface Team {
  id: TeamID
  name: NonEmptyString
  role: TeamRole
}

export type TeamID = string

export type TeamRole = "owner" | "write" | "read"

export interface TestEmail {
  from: string
  html: string
  id: NonEmptyString
  plain: string
  subject: string
  to: string
}

export type UnixMillis = number

export interface UpdateEpisode {
  description?: string
  enclosures?: readonly EpisodeEnclosure[]
  republished_at?: UnixMillis
  slug?: NonEmptyString
  status?: EpisodeStatus
  thumbnail_url?: string
  title?: NonEmptyString
}

export interface UpdateShow {
  description?: string
  slug?: NonEmptyString
  title?: NonEmptyString
  website_url?: string
}

export type UploadSessionID = string

export interface User {
  email: string
  id: UserID
  name?: NonEmptyString
  picture_url?: string
}

export type UserID = string

export interface ValidationErr {
  errors: readonly ValidationIssue[]
  message: NonEmptyString
}

export interface ValidationIssue {
  code: ValidationIssueCode
  field?: string
  in: ValidationLocation
  message: NonEmptyString
  path?: readonly string[]
}

export type ValidationIssueCode = string

export type ValidationLocation = "body" | "query" | "path" | "header" | "cookie"
