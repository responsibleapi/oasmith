import type {
  AppShellData,
  CompleteMediaMultipartUpload,
  CompletedMediaMultipartUpload,
  CreateAPIKey,
  CreateEpisode,
  CreateImageUploadPresign,
  CreateMediaMultipartUpload,
  CreateMediaUploadPresign,
  CreateShow,
  CreateTeam,
  CreatedAPIKey,
  CreatedImageUploadPresign,
  CreatedMediaMultipartUpload,
  CreatedMediaUploadPresign,
  EmailVerification,
  Episode,
  EpisodeID,
  EpisodeProcessingCompleteEvent,
  FeedID,
  GoogleSignup,
  ListedAPIKey,
  PasswordAuth,
  PasswordResetConfirmation,
  PasswordResetRequest,
  PresignMediaUploadParts,
  PresignedMediaUploadParts,
  Session,
  Show,
  ShowID,
  ShowPage,
  ShowsPage,
  Team,
  TeamID,
  TestEmail,
  UpdateEpisode,
  UpdateShow,
  UploadSessionID,
  User,
} from "./types.ts"

export type FetchInterceptor = (
  chain: FetchInterceptorChain,
) => Promise<Response>

export type FetchInterceptorChain = {
  request: Request
  proceed(request?: Request): Promise<Response>
}

export type ClientOptions = {
  baseURL: string
  fetch?: typeof globalThis.fetch
  interceptors?: FetchInterceptor[]
  responseTimeoutMs?: number
  sseIdleTimeoutMs?: number
  sseMaxRetries?: number
  sseReconnectBaseDelayMs?: number
  sseReconnectOnStreamEnd?: boolean
}

const defaultResponseTimeoutMs = 30_000

const defaultSSEIdleTimeoutMs = 60_000
const defaultSSEMaxRetries = 5
const defaultSSEReconnectBaseDelayMs = 200
const maxSSERetries = 100

export class RequiredError extends Error {
  constructor(
    public field: string,
    msg?: string,
  ) {
    super(msg)
    this.name = "RequiredError"
  }
}

export class ResponseError extends Error {
  constructor(
    public response: Response,
    msg?: string,
  ) {
    super(msg ?? "Response returned " + String(response.status))
    this.name = "ResponseError"
  }
}

type ResponseTimeout = {
  signal: AbortSignal
  stop(): void
}

const activeResponseTimeoutControllers = new Set<AbortController>()
const activeResponseTimeoutRequests = new Set<Request>()

function configuredTimeout(
  options: ClientOptions,
  name: "responseTimeoutMs" | "sseIdleTimeoutMs",
  defaultValue: number,
): number | undefined {
  const value = Object.prototype.hasOwnProperty.call(options, name)
    ? options[name]
    : defaultValue
  if (value === undefined || value === 0) {
    return undefined
  }
  if (!Number.isFinite(value) || value < 0) {
    throw new RangeError(`${name} must be a finite non-negative number`)
  }
  return value
}

function configuredSSEMaxRetries(options: ClientOptions): number {
  const value = Object.prototype.hasOwnProperty.call(options, "sseMaxRetries")
    ? options.sseMaxRetries
    : defaultSSEMaxRetries
  if (value === undefined) {
    return defaultSSEMaxRetries
  }
  if (!Number.isInteger(value) || value < 0 || value > maxSSERetries) {
    throw new RangeError(
      `sseMaxRetries must be an integer between 0 and ${maxSSERetries}`,
    )
  }
  return value
}

function configuredSSEReconnectBaseDelayMs(options: ClientOptions): number {
  const value = Object.prototype.hasOwnProperty.call(
    options,
    "sseReconnectBaseDelayMs",
  )
    ? options.sseReconnectBaseDelayMs
    : defaultSSEReconnectBaseDelayMs
  if (value === undefined) {
    return defaultSSEReconnectBaseDelayMs
  }
  if (!Number.isFinite(value) || value < 0) {
    throw new RangeError(
      "sseReconnectBaseDelayMs must be a finite non-negative number",
    )
  }
  return value
}

function responseTimeout(
  signal: AbortSignal,
  timeoutMs: number | undefined,
): ResponseTimeout {
  if (timeoutMs === undefined) {
    return { signal, stop() {} }
  }
  const controller = new AbortController()
  activeResponseTimeoutControllers.add(controller)
  function abortFromCaller(): void {
    controller.abort(signal.reason)
  }
  if (signal.aborted) {
    abortFromCaller()
  } else {
    signal.addEventListener("abort", abortFromCaller, { once: true })
  }
  const timer = setTimeout(() => {
    const error = new Error(`Response timed out after ${timeoutMs}ms`)
    error.name = "TimeoutError"
    controller.abort(error)
  }, timeoutMs)
  return {
    signal: controller.signal,
    stop(): void {
      clearTimeout(timer)
      signal.removeEventListener("abort", abortFromCaller)
      activeResponseTimeoutControllers.delete(controller)
    },
  }
}

function readEventStreamChunk(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  idleTimeoutMs: number | undefined,
): Promise<ReadableStreamReadResult<Uint8Array>> {
  if (idleTimeoutMs === undefined) {
    return reader.read()
  }
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      const error = new Error(`SSE stream was idle for ${idleTimeoutMs}ms`)
      error.name = "TimeoutError"
      void reader.cancel(error)
      reject(error)
    }, idleTimeoutMs)
    void reader.read().then(
      result => {
        clearTimeout(timer)
        resolve(result)
      },
      error => {
        clearTimeout(timer)
        reject(error)
      },
    )
  })
}

type SSEAttempt = {
  response: Response
  timeout: ResponseTimeout
}

function retryableSSEStatus(status: number): boolean {
  return status === 502 || status === 503 || status === 504
}

function sseError(message: string): Error {
  const error = new Error(message)
  error.name = "SSEReconnectError"
  return error
}

function sseRetryDelay(
  baseDelayMs: number,
  failureCount: number,
  serverDelayMs: number | undefined,
): number {
  if (serverDelayMs !== undefined) {
    return serverDelayMs
  }
  const exponential = baseDelayMs * 2 ** Math.max(0, failureCount - 1)
  return exponential * (0.9 + Math.random() * 0.2)
}

function waitForSSERetry(
  signal: AbortSignal,
  delayMs: number,
  isClosed?: () => boolean,
): Promise<void> {
  if (signal.aborted || isClosed?.()) {
    return Promise.reject(signal.reason ?? sseError("SSE reconnect canceled"))
  }
  return new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => {
        signal.removeEventListener("abort", abort)
        if (isClosed?.()) {
          reject(sseError("SSE reconnect canceled"))
        } else {
          resolve()
        }
      },
      Math.max(0, delayMs),
    )
    function abort(): void {
      clearTimeout(timer)
      signal.removeEventListener("abort", abort)
      reject(signal.reason ?? sseError("SSE reconnect canceled"))
    }
    signal.addEventListener("abort", abort, { once: true })
  })
}

async function openReconnectingSSE(
  open: () => Promise<SSEAttempt>,
  signal: AbortSignal,
  maxRetries: number,
  baseDelayMs: number,
): Promise<SSEAttempt> {
  let failures = 0
  let serverDelayMs: number | undefined
  while (true) {
    if (signal.aborted) {
      throw signal.reason ?? sseError("SSE connection canceled")
    }
    let attempt: SSEAttempt
    try {
      attempt = await open()
    } catch (error) {
      if (signal.aborted) {
        throw signal.reason ?? error
      }
      if (failures >= maxRetries) {
        throw error
      }
      failures += 1
      await waitForSSERetry(
        signal,
        sseRetryDelay(baseDelayMs, failures, serverDelayMs),
      )
      serverDelayMs = undefined
      continue
    }
    if (!retryableSSEStatus(attempt.response.status)) {
      return attempt
    }
    if (failures >= maxRetries) {
      return attempt
    }
    await attempt.response.body?.cancel()
    attempt.timeout.stop()
    failures += 1
    await waitForSSERetry(
      signal,
      sseRetryDelay(baseDelayMs, failures, serverDelayMs),
    )
    serverDelayMs = undefined
  }
}

function appendSSEBytes(
  existing: Uint8Array<ArrayBufferLike>,
  next: Uint8Array<ArrayBufferLike>,
): Uint8Array<ArrayBufferLike> {
  const combined = new Uint8Array(existing.byteLength + next.byteLength)
  combined.set(existing)
  combined.set(next, existing.byteLength)
  return combined
}

function sseFrameEnd(bytes: Uint8Array): number {
  for (let index = 1; index < bytes.byteLength; index += 1) {
    if (
      bytes[index] === 10 &&
      (bytes[index - 1] === 10 ||
        (bytes[index - 1] === 13 && index >= 2 && bytes[index - 2] === 10))
    ) {
      return index + 1
    }
  }
  return -1
}

function sseRetryValue(frame: Uint8Array): number | undefined {
  const text = new TextDecoder().decode(frame)
  for (const line of text.split(/\r?\n/)) {
    if (!line.startsWith("retry:")) {
      continue
    }
    const value = Number(line.slice(6).trim())
    if (Number.isInteger(value) && value >= 0 && Number.isFinite(value)) {
      return value
    }
  }
  return undefined
}

function reconnectingSSEBody(
  first: SSEAttempt,
  open: () => Promise<SSEAttempt>,
  signal: AbortSignal,
  maxRetries: number,
  baseDelayMs: number,
  idleTimeoutMs: number | undefined,
  reconnectOnStreamEnd: boolean,
  dispose: () => void,
): ReadableStream<Uint8Array> {
  let current = first
  let reader = current.response.body?.getReader()
  let pending: Uint8Array<ArrayBufferLike> = new Uint8Array()
  let readyFrames: Uint8Array<ArrayBufferLike>[] = []
  let failures = 0
  let serverDelayMs: number | undefined
  let closed = false

  async function closeCurrent(): Promise<void> {
    current.timeout.stop()
    try {
      await reader?.cancel()
    } catch {
      // The transport is already closing.
    }
    reader = undefined
  }

  async function reconnect(lastError: unknown): Promise<void> {
    while (true) {
      if (closed || signal.aborted) {
        throw signal.reason ?? lastError
      }
      if (failures >= maxRetries) {
        throw lastError
      }
      failures += 1
      await closeCurrent()
      await waitForSSERetry(
        signal,
        sseRetryDelay(baseDelayMs, failures, serverDelayMs),
        () => closed,
      )
      serverDelayMs = undefined
      try {
        const next = await open()
        if (retryableSSEStatus(next.response.status)) {
          lastError = sseError(
            `SSE reconnect returned HTTP ${next.response.status}`,
          )
          await next.response.body?.cancel()
          next.timeout.stop()
          if (failures >= maxRetries) {
            throw lastError
          }
          failures += 1
          await waitForSSERetry(
            signal,
            sseRetryDelay(baseDelayMs, failures, serverDelayMs),
            () => closed,
          )
          serverDelayMs = undefined
          continue
        }
        if (next.response.status < 200 || next.response.status >= 300) {
          await next.response.body?.cancel()
          next.timeout.stop()
          throw new ResponseError(
            next.response,
            `SSE reconnect returned HTTP ${next.response.status}`,
          )
        }
        current = next
        current.timeout.stop()
        reader = current.response.body?.getReader()
        pending = new Uint8Array()
        return
      } catch (error) {
        if (error instanceof ResponseError) {
          throw error
        }
        if (closed || signal.aborted) {
          throw signal.reason ?? error
        }
        lastError = error
      }
    }
  }

  return new ReadableStream<Uint8Array>({
    async pull(controller): Promise<void> {
      if (closed) {
        return
      }
      if (readyFrames.length > 0) {
        controller.enqueue(readyFrames.shift()!)
        return
      }
      while (!closed) {
        try {
          if (!reader) {
            throw sseError("SSE response has no readable body")
          }
          const result = await readEventStreamChunk(reader, idleTimeoutMs)
          if (result.done) {
            if (!reconnectOnStreamEnd) {
              closed = true
              dispose()
              controller.close()
              return
            }
            throw sseError("SSE stream ended before a complete event")
          }
          pending = appendSSEBytes(pending, result.value)
          let end = sseFrameEnd(pending)
          while (end !== -1) {
            const frame = pending.slice(0, end)
            pending = pending.slice(end)
            const retry = sseRetryValue(frame)
            if (retry !== undefined) {
              serverDelayMs = retry
            }
            failures = 0
            readyFrames.push(frame)
            end = sseFrameEnd(pending)
          }
          if (readyFrames.length > 0) {
            controller.enqueue(readyFrames.shift()!)
            return
          }
        } catch (error) {
          if (closed || signal.aborted) {
            controller.error(signal.reason ?? error)
            return
          }
          pending = new Uint8Array()
          try {
            await reconnect(error)
          } catch (reconnectError) {
            closed = true
            dispose()
            controller.error(reconnectError)
            return
          }
        }
      }
    },
    async cancel(): Promise<void> {
      closed = true
      readyFrames = []
      pending = new Uint8Array()
      await closeCurrent()
      dispose()
    },
  })
}

async function* parseEventStream<T>(
  response: Response,
  idleTimeoutMs: number | undefined,
): AsyncGenerator<T, void, void> {
  const reader = response.body?.getReader()
  if (!reader) {
    return
  }
  const decoder = new TextDecoder()
  let buffer = ""
  let data: string[] = []
  try {
    while (true) {
      const result = await readEventStreamChunk(reader, idleTimeoutMs)
      if (result.done) {
        break
      }
      buffer += decoder.decode(result.value, { stream: true })
      let newlineIndex = buffer.indexOf("\n")
      while (newlineIndex !== -1) {
        const rawLine = buffer.slice(0, newlineIndex)
        buffer = buffer.slice(newlineIndex + 1)
        const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine
        if (line === "") {
          if (data.length > 0) {
            const parsed: T = JSON.parse(data.join("\n"))
            yield parsed
            data = []
          }
        } else if (line.startsWith("data:")) {
          data.push(line.slice(5).trimStart())
        }
        newlineIndex = buffer.indexOf("\n")
      }
    }
    buffer += decoder.decode()
    if (buffer.startsWith("data:")) {
      data.push(buffer.slice(5).trimStart())
    }
    if (data.length > 0) {
      const parsed: T = JSON.parse(data.join("\n"))
      yield parsed
    }
  } finally {
    try {
      await reader.cancel()
    } catch {
      // The request signal may already have canceled the body.
    }
    reader.releaseLock()
  }
}

async function runInterceptors(
  request: Request,
  interceptors: FetchInterceptor[],
  fetch: typeof globalThis.fetch,
): Promise<Response> {
  async function dispatch(
    index: number,
    nextRequest: Request,
  ): Promise<Response> {
    const interceptor = interceptors[index]
    if (!interceptor) {
      return await fetch(nextRequest)
    }
    return await interceptor({
      request: nextRequest,
      proceed(requestOverride?: Request): Promise<Response> {
        return dispatch(index + 1, requestOverride ?? nextRequest)
      },
    })
  }
  return await dispatch(0, request)
}

export interface VerifyEmailRequest {
  emailVerification: EmailVerification
}

export interface CreateGoogleSignupSessionRequest {
  googleSignup: GoogleSignup
}

export interface CreatePasswordLoginSessionRequest {
  passwordAuth: PasswordAuth
}

export interface ConfirmPasswordResetRequest {
  passwordResetConfirmation: PasswordResetConfirmation
}

export interface RequestPasswordResetRequest {
  passwordResetRequest: PasswordResetRequest
}

export interface CreatePasswordSignupSessionRequest {
  passwordAuth: PasswordAuth
}

export interface CreateAPIKeyRequest {
  createAPIKey: CreateAPIKey
}

export interface ShowPageRequest {
  showId: ShowID
}

export interface ShowsPageRequest {
  teamId: TeamID
}

export interface CreateTeamRequest {
  createTeam: CreateTeam
}

export interface CreateImageUploadPresignRequest {
  teamId: TeamID
  createImageUploadPresign: CreateImageUploadPresign
}

export interface CreateMediaMultipartUploadRequest {
  teamId: TeamID
  createMediaMultipartUpload: CreateMediaMultipartUpload
}

export interface CreateMediaUploadPresignRequest {
  teamId: TeamID
  createMediaUploadPresign: CreateMediaUploadPresign
}

export interface CompleteMediaMultipartUploadRequest {
  teamId: TeamID
  uploadSessionId: UploadSessionID
  completeMediaMultipartUpload: CompleteMediaMultipartUpload
}

export interface PresignMediaUploadPartsRequest {
  teamId: TeamID
  uploadSessionId: UploadSessionID
  presignMediaUploadParts: PresignMediaUploadParts
}

export interface ListShowsRequest {
  teamId: TeamID
}

export interface CreateShowRequest {
  teamId: TeamID
  createShow: CreateShow
}

export interface UpdateShowRequest {
  teamId: TeamID
  showId: ShowID
  updateShow: UpdateShow
}

export interface ListEpisodesRequest {
  teamId: TeamID
  showId: ShowID
}

export interface CreateEpisodeRequest {
  teamId: TeamID
  showId: ShowID
  createEpisode: CreateEpisode
}

export interface UpdateEpisodeRequest {
  teamId: TeamID
  showId: ShowID
  episodeId: EpisodeID
  updateEpisode: UpdateEpisode
}

export interface EpisodeProcessingEventsRequest {
  teamId: TeamID
  showId: ShowID
  episodeId: EpisodeID
}

export interface RenderShowRSSFeedRequest {
  feedId: FeedID
}

export class DefaultApi {
  private baseURL: string
  private fetch: typeof globalThis.fetch
  private interceptors: FetchInterceptor[]
  private responseTimeoutMs: number | undefined

  private sseIdleTimeoutMs: number | undefined
  private sseMaxRetries: number
  private sseReconnectBaseDelayMs: number
  private sseReconnectOnStreamEnd: boolean

  constructor(options: ClientOptions) {
    this.baseURL = options.baseURL
    this.fetch = options.fetch ?? globalThis.fetch
    this.interceptors = options.interceptors ?? []
    this.responseTimeoutMs = configuredTimeout(
      options,
      "responseTimeoutMs",
      defaultResponseTimeoutMs,
    )
    this.sseIdleTimeoutMs = configuredTimeout(
      options,
      "sseIdleTimeoutMs",
      defaultSSEIdleTimeoutMs,
    )
    this.sseMaxRetries = configuredSSEMaxRetries(options)
    this.sseReconnectBaseDelayMs = configuredSSEReconnectBaseDelayMs(options)
    this.sseReconnectOnStreamEnd = options.sseReconnectOnStreamEnd ?? true
  }

  private async request(
    request: Request,
    initOverrides?: RequestInit,
  ): Promise<{
    response: Response
    timeout: ResponseTimeout
    request: Request
  }> {
    const callerSignal = initOverrides?.signal ?? request.signal
    const timeout = responseTimeout(callerSignal, this.responseTimeoutMs)
    const finalRequest = new Request(request, {
      ...initOverrides,
      signal: timeout.signal,
    })
    activeResponseTimeoutRequests.add(finalRequest)
    const timedRequest = {
      signal: timeout.signal,
      stop(): void {
        activeResponseTimeoutRequests.delete(finalRequest)
        timeout.stop()
      },
    }
    try {
      return {
        response: await runInterceptors(
          finalRequest,
          this.interceptors,
          this.fetch,
        ),
        timeout: timedRequest,
        request: finalRequest,
      }
    } catch (error) {
      timedRequest.stop()
      throw error
    }
  }

  private async sseRequest(
    request: Request,
    initOverrides?: RequestInit,
  ): Promise<{
    attempt: SSEAttempt
    signal: AbortSignal
    open(): Promise<SSEAttempt>
    dispose(): void
  }> {
    const callerSignal = initOverrides?.signal ?? request.signal
    const controller = new AbortController()
    const abortFromCaller = (): void => controller.abort(callerSignal.reason)
    if (callerSignal.aborted) {
      abortFromCaller()
    } else {
      callerSignal.addEventListener("abort", abortFromCaller, { once: true })
    }
    let disposed = false
    const dispose = (): void => {
      if (disposed) {
        return
      }
      disposed = true
      callerSignal.removeEventListener("abort", abortFromCaller)
      controller.abort()
    }
    let replayRequest = request
    const open = async (): Promise<SSEAttempt> => {
      const opened = await this.request(replayRequest, {
        ...initOverrides,
        signal: controller.signal,
      })
      replayRequest = opened.request
      return { response: opened.response, timeout: opened.timeout }
    }
    try {
      const attempt = await openReconnectingSSE(
        open,
        controller.signal,
        this.sseMaxRetries,
        this.sseReconnectBaseDelayMs,
      )
      return { attempt, signal: controller.signal, open, dispose }
    } catch (error) {
      dispose()
      throw error
    }
  }

  verifyEmailRequest(requestParameters: VerifyEmailRequest): Request {
    if (
      requestParameters["emailVerification"] === null ||
      requestParameters["emailVerification"] === undefined
    ) {
      throw new RequiredError(
        "emailVerification",
        'Required parameter "emailVerification" was null or undefined when calling verifyEmail().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/email/verify", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["emailVerification"]),
    })
  }

  async verifyEmailResult(
    requestParameters: VerifyEmailRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Session; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.verifyEmailRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Session = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async verifyEmail(
    emailVerification: EmailVerification,
    initOverrides?: RequestInit,
  ): Promise<Session> {
    const response = await this.verifyEmailResult(
      { emailVerification },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createGoogleSignupSessionRequest(
    requestParameters: CreateGoogleSignupSessionRequest,
  ): Request {
    if (
      requestParameters["googleSignup"] === null ||
      requestParameters["googleSignup"] === undefined
    ) {
      throw new RequiredError(
        "googleSignup",
        'Required parameter "googleSignup" was null or undefined when calling createGoogleSignupSession().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/google/signup", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["googleSignup"]),
    })
  }

  async createGoogleSignupSessionResult(
    requestParameters: CreateGoogleSignupSessionRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Session; raw: Response }
    | { status: 400; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createGoogleSignupSessionRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Session = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createGoogleSignupSession(
    googleSignup: GoogleSignup,
    initOverrides?: RequestInit,
  ): Promise<Session> {
    const response = await this.createGoogleSignupSessionResult(
      { googleSignup },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createPasswordLoginSessionRequest(
    requestParameters: CreatePasswordLoginSessionRequest,
  ): Request {
    if (
      requestParameters["passwordAuth"] === null ||
      requestParameters["passwordAuth"] === undefined
    ) {
      throw new RequiredError(
        "passwordAuth",
        'Required parameter "passwordAuth" was null or undefined when calling createPasswordLoginSession().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/login", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["passwordAuth"]),
    })
  }

  async createPasswordLoginSessionResult(
    requestParameters: CreatePasswordLoginSessionRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Session; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 403; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createPasswordLoginSessionRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Session = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 403) {
        return { status: 403, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createPasswordLoginSession(
    passwordAuth: PasswordAuth,
    initOverrides?: RequestInit,
  ): Promise<Session> {
    const response = await this.createPasswordLoginSessionResult(
      { passwordAuth },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  confirmPasswordResetRequest(
    requestParameters: ConfirmPasswordResetRequest,
  ): Request {
    if (
      requestParameters["passwordResetConfirmation"] === null ||
      requestParameters["passwordResetConfirmation"] === undefined
    ) {
      throw new RequiredError(
        "passwordResetConfirmation",
        'Required parameter "passwordResetConfirmation" was null or undefined when calling confirmPasswordReset().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/password/reset/confirm", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["passwordResetConfirmation"]),
    })
  }

  async confirmPasswordResetResult(
    requestParameters: ConfirmPasswordResetRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Session; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.confirmPasswordResetRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Session = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async confirmPasswordReset(
    passwordResetConfirmation: PasswordResetConfirmation,
    initOverrides?: RequestInit,
  ): Promise<Session> {
    const response = await this.confirmPasswordResetResult(
      { passwordResetConfirmation },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  requestPasswordResetRequest(
    requestParameters: RequestPasswordResetRequest,
  ): Request {
    if (
      requestParameters["passwordResetRequest"] === null ||
      requestParameters["passwordResetRequest"] === undefined
    ) {
      throw new RequiredError(
        "passwordResetRequest",
        'Required parameter "passwordResetRequest" was null or undefined when calling requestPasswordReset().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/password/reset/request", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["passwordResetRequest"]),
    })
  }

  async requestPasswordResetResult(
    requestParameters: RequestPasswordResetRequest,
    initOverrides?: RequestInit,
  ): Promise<{ status: 202; raw: Response } | { status: 400; raw: Response }> {
    const timedResponse = await this.request(
      this.requestPasswordResetRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 202) {
        return { status: 202, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async requestPasswordReset(
    passwordResetRequest: PasswordResetRequest,
    initOverrides?: RequestInit,
  ): Promise<void> {
    const response = await this.requestPasswordResetResult(
      { passwordResetRequest },
      initOverrides,
    )
    if (response.status === 202) {
      return
    }
    throw new ResponseError(response.raw)
  }

  createPasswordSignupSessionRequest(
    requestParameters: CreatePasswordSignupSessionRequest,
  ): Request {
    if (
      requestParameters["passwordAuth"] === null ||
      requestParameters["passwordAuth"] === undefined
    ) {
      throw new RequiredError(
        "passwordAuth",
        'Required parameter "passwordAuth" was null or undefined when calling createPasswordSignupSession().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/auth/signup", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["passwordAuth"]),
    })
  }

  async createPasswordSignupSessionResult(
    requestParameters: CreatePasswordSignupSessionRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 202; raw: Response }
    | { status: 400; raw: Response }
    | { status: 409; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createPasswordSignupSessionRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 202) {
        return { status: 202, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 409) {
        return { status: 409, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createPasswordSignupSession(
    passwordAuth: PasswordAuth,
    initOverrides?: RequestInit,
  ): Promise<void> {
    const response = await this.createPasswordSignupSessionResult(
      { passwordAuth },
      initOverrides,
    )
    if (response.status === 202) {
      return
    }
    throw new ResponseError(response.raw)
  }

  whoamiRequest(): Request {
    const headerParameters: Record<string, string> = {}
    return new Request(this.baseURL + "/session", {
      method: "GET",
      headers: headerParameters,
    })
  }

  async whoamiResult(
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: User; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.whoamiRequest(),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: User = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async whoami(initOverrides?: RequestInit): Promise<User> {
    const response = await this.whoamiResult(initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  listAPIKeysRequest(): Request {
    const headerParameters: Record<string, string> = {}
    return new Request(this.baseURL + "/session/api-keys", {
      method: "GET",
      headers: headerParameters,
    })
  }

  async listAPIKeysResult(
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: readonly ListedAPIKey[]; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.listAPIKeysRequest(),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: readonly ListedAPIKey[] = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async listAPIKeys(
    initOverrides?: RequestInit,
  ): Promise<readonly ListedAPIKey[]> {
    const response = await this.listAPIKeysResult(initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createAPIKeyRequest(requestParameters: CreateAPIKeyRequest): Request {
    if (
      requestParameters["createAPIKey"] === null ||
      requestParameters["createAPIKey"] === undefined
    ) {
      throw new RequiredError(
        "createAPIKey",
        'Required parameter "createAPIKey" was null or undefined when calling createAPIKey().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/session/api-keys", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["createAPIKey"]),
    })
  }

  async createAPIKeyResult(
    requestParameters: CreateAPIKeyRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: CreatedAPIKey; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createAPIKeyRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: CreatedAPIKey = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createAPIKey(
    createAPIKey: CreateAPIKey,
    initOverrides?: RequestInit,
  ): Promise<CreatedAPIKey> {
    const response = await this.createAPIKeyResult(
      { createAPIKey },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  appShellRequest(): Request {
    const headerParameters: Record<string, string> = {}
    return new Request(this.baseURL + "/session/app-shell", {
      method: "GET",
      headers: headerParameters,
    })
  }

  async appShellResult(
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: AppShellData; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.appShellRequest(),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: AppShellData = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async appShell(initOverrides?: RequestInit): Promise<AppShellData> {
    const response = await this.appShellResult(initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  defaultTeamRequest(): Request {
    const headerParameters: Record<string, string> = {}
    return new Request(this.baseURL + "/session/default-team", {
      method: "GET",
      headers: headerParameters,
    })
  }

  async defaultTeamResult(
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: Team; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.defaultTeamRequest(),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: Team = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async defaultTeam(initOverrides?: RequestInit): Promise<Team> {
    const response = await this.defaultTeamResult(initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  showPageRequest(requestParameters: ShowPageRequest): Request {
    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling showPage().',
      )
    }

    const headerParameters: Record<string, string> = {}
    return new Request(
      this.baseURL +
        `/session/pages/show/${encodeURIComponent(requestParameters["showId"])}`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async showPageResult(
    requestParameters: ShowPageRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: ShowPage; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.showPageRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: ShowPage = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async showPage(
    showId: ShowID,
    initOverrides?: RequestInit,
  ): Promise<ShowPage> {
    const response = await this.showPageResult({ showId }, initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  showsPageRequest(requestParameters: ShowsPageRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling showsPage().',
      )
    }

    const headerParameters: Record<string, string> = {}
    return new Request(
      this.baseURL +
        `/session/pages/shows/${encodeURIComponent(requestParameters["teamId"])}`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async showsPageResult(
    requestParameters: ShowsPageRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: ShowsPage; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.showsPageRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: ShowsPage = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async showsPage(
    teamId: TeamID,
    initOverrides?: RequestInit,
  ): Promise<ShowsPage> {
    const response = await this.showsPageResult({ teamId }, initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createTeamRequest(requestParameters: CreateTeamRequest): Request {
    if (
      requestParameters["createTeam"] === null ||
      requestParameters["createTeam"] === undefined
    ) {
      throw new RequiredError(
        "createTeam",
        'Required parameter "createTeam" was null or undefined when calling createTeam().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(this.baseURL + "/session/teams", {
      method: "POST",
      headers: headerParameters,
      body: JSON.stringify(requestParameters["createTeam"]),
    })
  }

  async createTeamResult(
    requestParameters: CreateTeamRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Team; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createTeamRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Team = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createTeam(
    createTeam: CreateTeam,
    initOverrides?: RequestInit,
  ): Promise<Team> {
    const response = await this.createTeamResult({ createTeam }, initOverrides)
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createImageUploadPresignRequest(
    requestParameters: CreateImageUploadPresignRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling createImageUploadPresign().',
      )
    }

    if (
      requestParameters["createImageUploadPresign"] === null ||
      requestParameters["createImageUploadPresign"] === undefined
    ) {
      throw new RequiredError(
        "createImageUploadPresign",
        'Required parameter "createImageUploadPresign" was null or undefined when calling createImageUploadPresign().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/image-uploads/presign`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["createImageUploadPresign"]),
      },
    )
  }

  async createImageUploadPresignResult(
    requestParameters: CreateImageUploadPresignRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: CreatedImageUploadPresign; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createImageUploadPresignRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: CreatedImageUploadPresign = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createImageUploadPresign(
    teamId: TeamID,
    createImageUploadPresign: CreateImageUploadPresign,
    initOverrides?: RequestInit,
  ): Promise<CreatedImageUploadPresign> {
    const response = await this.createImageUploadPresignResult(
      { teamId, createImageUploadPresign },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createMediaMultipartUploadRequest(
    requestParameters: CreateMediaMultipartUploadRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling createMediaMultipartUpload().',
      )
    }

    if (
      requestParameters["createMediaMultipartUpload"] === null ||
      requestParameters["createMediaMultipartUpload"] === undefined
    ) {
      throw new RequiredError(
        "createMediaMultipartUpload",
        'Required parameter "createMediaMultipartUpload" was null or undefined when calling createMediaMultipartUpload().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/media-uploads/multipart`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["createMediaMultipartUpload"]),
      },
    )
  }

  async createMediaMultipartUploadResult(
    requestParameters: CreateMediaMultipartUploadRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: CreatedMediaMultipartUpload; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createMediaMultipartUploadRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: CreatedMediaMultipartUpload = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createMediaMultipartUpload(
    teamId: TeamID,
    createMediaMultipartUpload: CreateMediaMultipartUpload,
    initOverrides?: RequestInit,
  ): Promise<CreatedMediaMultipartUpload> {
    const response = await this.createMediaMultipartUploadResult(
      { teamId, createMediaMultipartUpload },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createMediaUploadPresignRequest(
    requestParameters: CreateMediaUploadPresignRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling createMediaUploadPresign().',
      )
    }

    if (
      requestParameters["createMediaUploadPresign"] === null ||
      requestParameters["createMediaUploadPresign"] === undefined
    ) {
      throw new RequiredError(
        "createMediaUploadPresign",
        'Required parameter "createMediaUploadPresign" was null or undefined when calling createMediaUploadPresign().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/media-uploads/presign`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["createMediaUploadPresign"]),
      },
    )
  }

  async createMediaUploadPresignResult(
    requestParameters: CreateMediaUploadPresignRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: CreatedMediaUploadPresign; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createMediaUploadPresignRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: CreatedMediaUploadPresign = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createMediaUploadPresign(
    teamId: TeamID,
    createMediaUploadPresign: CreateMediaUploadPresign,
    initOverrides?: RequestInit,
  ): Promise<CreatedMediaUploadPresign> {
    const response = await this.createMediaUploadPresignResult(
      { teamId, createMediaUploadPresign },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  completeMediaMultipartUploadRequest(
    requestParameters: CompleteMediaMultipartUploadRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling completeMediaMultipartUpload().',
      )
    }

    if (
      requestParameters["uploadSessionId"] === null ||
      requestParameters["uploadSessionId"] === undefined
    ) {
      throw new RequiredError(
        "uploadSessionId",
        'Required parameter "uploadSessionId" was null or undefined when calling completeMediaMultipartUpload().',
      )
    }

    if (
      requestParameters["completeMediaMultipartUpload"] === null ||
      requestParameters["completeMediaMultipartUpload"] === undefined
    ) {
      throw new RequiredError(
        "completeMediaMultipartUpload",
        'Required parameter "completeMediaMultipartUpload" was null or undefined when calling completeMediaMultipartUpload().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/media-uploads/${encodeURIComponent(requestParameters["uploadSessionId"])}/complete`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["completeMediaMultipartUpload"]),
      },
    )
  }

  async completeMediaMultipartUploadResult(
    requestParameters: CompleteMediaMultipartUploadRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: CompletedMediaMultipartUpload; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.completeMediaMultipartUploadRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: CompletedMediaMultipartUpload = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async completeMediaMultipartUpload(
    teamId: TeamID,
    uploadSessionId: UploadSessionID,
    completeMediaMultipartUpload: CompleteMediaMultipartUpload,
    initOverrides?: RequestInit,
  ): Promise<CompletedMediaMultipartUpload> {
    const response = await this.completeMediaMultipartUploadResult(
      { teamId, uploadSessionId, completeMediaMultipartUpload },
      initOverrides,
    )
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  presignMediaUploadPartsRequest(
    requestParameters: PresignMediaUploadPartsRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling presignMediaUploadParts().',
      )
    }

    if (
      requestParameters["uploadSessionId"] === null ||
      requestParameters["uploadSessionId"] === undefined
    ) {
      throw new RequiredError(
        "uploadSessionId",
        'Required parameter "uploadSessionId" was null or undefined when calling presignMediaUploadParts().',
      )
    }

    if (
      requestParameters["presignMediaUploadParts"] === null ||
      requestParameters["presignMediaUploadParts"] === undefined
    ) {
      throw new RequiredError(
        "presignMediaUploadParts",
        'Required parameter "presignMediaUploadParts" was null or undefined when calling presignMediaUploadParts().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/media-uploads/${encodeURIComponent(requestParameters["uploadSessionId"])}/parts/presign`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["presignMediaUploadParts"]),
      },
    )
  }

  async presignMediaUploadPartsResult(
    requestParameters: PresignMediaUploadPartsRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: PresignedMediaUploadParts; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.presignMediaUploadPartsRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: PresignedMediaUploadParts = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async presignMediaUploadParts(
    teamId: TeamID,
    uploadSessionId: UploadSessionID,
    presignMediaUploadParts: PresignMediaUploadParts,
    initOverrides?: RequestInit,
  ): Promise<PresignedMediaUploadParts> {
    const response = await this.presignMediaUploadPartsResult(
      { teamId, uploadSessionId, presignMediaUploadParts },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  listShowsRequest(requestParameters: ListShowsRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling listShows().',
      )
    }

    const headerParameters: Record<string, string> = {}
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async listShowsResult(
    requestParameters: ListShowsRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: readonly Show[]; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.listShowsRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: readonly Show[] = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async listShows(
    teamId: TeamID,
    initOverrides?: RequestInit,
  ): Promise<readonly Show[]> {
    const response = await this.listShowsResult({ teamId }, initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createShowRequest(requestParameters: CreateShowRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling createShow().',
      )
    }

    if (
      requestParameters["createShow"] === null ||
      requestParameters["createShow"] === undefined
    ) {
      throw new RequiredError(
        "createShow",
        'Required parameter "createShow" was null or undefined when calling createShow().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["createShow"]),
      },
    )
  }

  async createShowResult(
    requestParameters: CreateShowRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Show; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 403; raw: Response }
    | { status: 409; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createShowRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Show = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 403) {
        return { status: 403, raw: response }
      }
      if (response.status === 409) {
        return { status: 409, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createShow(
    teamId: TeamID,
    createShow: CreateShow,
    initOverrides?: RequestInit,
  ): Promise<Show> {
    const response = await this.createShowResult(
      { teamId, createShow },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  updateShowRequest(requestParameters: UpdateShowRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling updateShow().',
      )
    }

    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling updateShow().',
      )
    }

    if (
      requestParameters["updateShow"] === null ||
      requestParameters["updateShow"] === undefined
    ) {
      throw new RequiredError(
        "updateShow",
        'Required parameter "updateShow" was null or undefined when calling updateShow().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows/${encodeURIComponent(requestParameters["showId"])}`,
      {
        method: "PUT",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["updateShow"]),
      },
    )
  }

  async updateShowResult(
    requestParameters: UpdateShowRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: Show; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.updateShowRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: Show = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async updateShow(
    teamId: TeamID,
    showId: ShowID,
    updateShow: UpdateShow,
    initOverrides?: RequestInit,
  ): Promise<Show> {
    const response = await this.updateShowResult(
      { teamId, showId, updateShow },
      initOverrides,
    )
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  listEpisodesRequest(requestParameters: ListEpisodesRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling listEpisodes().',
      )
    }

    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling listEpisodes().',
      )
    }

    const headerParameters: Record<string, string> = {}
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows/${encodeURIComponent(requestParameters["showId"])}/episodes`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async listEpisodesResult(
    requestParameters: ListEpisodesRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: readonly Episode[]; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.listEpisodesRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: readonly Episode[] = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async listEpisodes(
    teamId: TeamID,
    showId: ShowID,
    initOverrides?: RequestInit,
  ): Promise<readonly Episode[]> {
    const response = await this.listEpisodesResult(
      { teamId, showId },
      initOverrides,
    )
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  createEpisodeRequest(requestParameters: CreateEpisodeRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling createEpisode().',
      )
    }

    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling createEpisode().',
      )
    }

    if (
      requestParameters["createEpisode"] === null ||
      requestParameters["createEpisode"] === undefined
    ) {
      throw new RequiredError(
        "createEpisode",
        'Required parameter "createEpisode" was null or undefined when calling createEpisode().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows/${encodeURIComponent(requestParameters["showId"])}/episodes`,
      {
        method: "POST",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["createEpisode"]),
      },
    )
  }

  async createEpisodeResult(
    requestParameters: CreateEpisodeRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 201; body: Episode; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.createEpisodeRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 201) {
        const body: Episode = await response.json()
        return { status: 201, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async createEpisode(
    teamId: TeamID,
    showId: ShowID,
    createEpisode: CreateEpisode,
    initOverrides?: RequestInit,
  ): Promise<Episode> {
    const response = await this.createEpisodeResult(
      { teamId, showId, createEpisode },
      initOverrides,
    )
    if (response.status === 201) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  updateEpisodeRequest(requestParameters: UpdateEpisodeRequest): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling updateEpisode().',
      )
    }

    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling updateEpisode().',
      )
    }

    if (
      requestParameters["episodeId"] === null ||
      requestParameters["episodeId"] === undefined
    ) {
      throw new RequiredError(
        "episodeId",
        'Required parameter "episodeId" was null or undefined when calling updateEpisode().',
      )
    }

    if (
      requestParameters["updateEpisode"] === null ||
      requestParameters["updateEpisode"] === undefined
    ) {
      throw new RequiredError(
        "updateEpisode",
        'Required parameter "updateEpisode" was null or undefined when calling updateEpisode().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Content-Type"] = "application/json"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows/${encodeURIComponent(requestParameters["showId"])}/episodes/${encodeURIComponent(requestParameters["episodeId"])}`,
      {
        method: "PUT",
        headers: headerParameters,
        body: JSON.stringify(requestParameters["updateEpisode"]),
      },
    )
  }

  async updateEpisodeResult(
    requestParameters: UpdateEpisodeRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: Episode; raw: Response }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
  > {
    const timedResponse = await this.request(
      this.updateEpisodeRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: Episode = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async updateEpisode(
    teamId: TeamID,
    showId: ShowID,
    episodeId: EpisodeID,
    updateEpisode: UpdateEpisode,
    initOverrides?: RequestInit,
  ): Promise<Episode> {
    const response = await this.updateEpisodeResult(
      { teamId, showId, episodeId, updateEpisode },
      initOverrides,
    )
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  episodeProcessingEventsRequest(
    requestParameters: EpisodeProcessingEventsRequest,
  ): Request {
    if (
      requestParameters["teamId"] === null ||
      requestParameters["teamId"] === undefined
    ) {
      throw new RequiredError(
        "teamId",
        'Required parameter "teamId" was null or undefined when calling episodeProcessingEvents().',
      )
    }

    if (
      requestParameters["showId"] === null ||
      requestParameters["showId"] === undefined
    ) {
      throw new RequiredError(
        "showId",
        'Required parameter "showId" was null or undefined when calling episodeProcessingEvents().',
      )
    }

    if (
      requestParameters["episodeId"] === null ||
      requestParameters["episodeId"] === undefined
    ) {
      throw new RequiredError(
        "episodeId",
        'Required parameter "episodeId" was null or undefined when calling episodeProcessingEvents().',
      )
    }

    const headerParameters: Record<string, string> = {}
    headerParameters["Accept"] = "text/event-stream"
    return new Request(
      this.baseURL +
        `/session/teams/${encodeURIComponent(requestParameters["teamId"])}/shows/${encodeURIComponent(requestParameters["showId"])}/episodes/${encodeURIComponent(requestParameters["episodeId"])}/episode-events`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async episodeProcessingEventsResult(
    requestParameters: EpisodeProcessingEventsRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | {
        status: 200
        body: AsyncIterable<EpisodeProcessingCompleteEvent>
        raw: Response
      }
    | { status: 400; raw: Response }
    | { status: 401; raw: Response }
    | { status: 403; raw: Response }
    | { status: 404; raw: Response }
  > {
    const sseResponse = await this.sseRequest(
      this.episodeProcessingEventsRequest(requestParameters),
      initOverrides,
    )
    const timedResponse = sseResponse.attempt
    let keepSSE = false

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        timedResponse.timeout.stop()
        const raw = new Response(
          reconnectingSSEBody(
            timedResponse,
            () => sseResponse.open(),
            sseResponse.signal,
            this.sseMaxRetries,
            this.sseReconnectBaseDelayMs,
            this.sseIdleTimeoutMs,
            this.sseReconnectOnStreamEnd,
            () => sseResponse.dispose(),
          ),
          {
            status: response.status,
            statusText: response.statusText,
            headers: response.headers,
          },
        )
        keepSSE = true
        return {
          status: 200,
          body: parseEventStream<EpisodeProcessingCompleteEvent>(
            raw,
            undefined,
          ),
          raw,
        }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 401) {
        return { status: 401, raw: response }
      }
      if (response.status === 403) {
        return { status: 403, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
      if (!keepSSE) {
        sseResponse.dispose()
      }
    }
  }

  async *episodeProcessingEvents(
    teamId: TeamID,
    showId: ShowID,
    episodeId: EpisodeID,
    initOverrides?: RequestInit,
  ): AsyncGenerator<EpisodeProcessingCompleteEvent, void, void> {
    const response = await this.episodeProcessingEventsResult(
      { teamId, showId, episodeId },
      initOverrides,
    )
    if (response.status === 200) {
      yield* response.body
      return
    }
    throw new ResponseError(response.raw)
  }

  listTestEmailsRequest(): Request {
    const headerParameters: Record<string, string> = {}
    return new Request(this.baseURL + "/test/emails", {
      method: "GET",
      headers: headerParameters,
    })
  }

  async listTestEmailsResult(
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: readonly TestEmail[]; raw: Response }
    | { status: 400; raw: Response }
    | { status: 404; raw: Response }
  > {
    const timedResponse = await this.request(
      this.listTestEmailsRequest(),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: readonly TestEmail[] = await response.json()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      if (response.status === 404) {
        return { status: 404, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async listTestEmails(
    initOverrides?: RequestInit,
  ): Promise<readonly TestEmail[]> {
    const response = await this.listTestEmailsResult(initOverrides)
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }

  renderShowRSSFeedRequest(
    requestParameters: RenderShowRSSFeedRequest,
  ): Request {
    if (
      requestParameters["feedId"] === null ||
      requestParameters["feedId"] === undefined
    ) {
      throw new RequiredError(
        "feedId",
        'Required parameter "feedId" was null or undefined when calling renderShowRSSFeed().',
      )
    }

    const headerParameters: Record<string, string> = {}
    return new Request(
      this.baseURL + `/${encodeURIComponent(requestParameters["feedId"])}.rss`,
      {
        method: "GET",
        headers: headerParameters,
      },
    )
  }

  async renderShowRSSFeedResult(
    requestParameters: RenderShowRSSFeedRequest,
    initOverrides?: RequestInit,
  ): Promise<
    | { status: 200; body: string; raw: Response }
    | { status: 400; raw: Response }
  > {
    const timedResponse = await this.request(
      this.renderShowRSSFeedRequest(requestParameters),
      initOverrides,
    )

    const response = timedResponse.response
    try {
      if (response.status === 200) {
        const body: string = await response.text()
        return { status: 200, body, raw: response }
      }
      if (response.status === 400) {
        return { status: 400, raw: response }
      }
      throw new ResponseError(
        response,
        `Unexpected response status ${response.status}`,
      )
    } finally {
      timedResponse.timeout.stop()
    }
  }

  async renderShowRSSFeed(
    feedId: FeedID,
    initOverrides?: RequestInit,
  ): Promise<string> {
    const response = await this.renderShowRSSFeedResult(
      { feedId },
      initOverrides,
    )
    if (response.status === 200) {
      return response.body
    }
    throw new ResponseError(response.raw)
  }
}
