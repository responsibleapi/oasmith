export type FetchInterceptor = (
  chain: FetchInterceptorChain,
) => Promise<Response>

export type FetchInterceptorChain = {
  request: Request
  proceed(request?: Request): Promise<Response>
}

export type ClientOptions = {
  baseURL?: string
  interceptors?: FetchInterceptor[]
  responseTimeoutMs?: number
  sseIdleTimeoutMs?: number
  sseMaxRetries?: number
  sseReconnectBaseDelayMs?: number
  sseReconnectOnStreamEnd?: boolean
}

const defaultResponseTimeoutMs = 30_000

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

async function runInterceptors(
  request: Request,
  interceptors: FetchInterceptor[],
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

export class DefaultApi {
  private baseURL: string
  private interceptors: FetchInterceptor[]
  private responseTimeoutMs: number | undefined

  constructor(options: ClientOptions = {}) {
    this.baseURL = options.baseURL ?? ""
    this.interceptors = options.interceptors ?? []
    this.responseTimeoutMs = configuredTimeout(
      options,
      "responseTimeoutMs",
      defaultResponseTimeoutMs,
    )
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
        response: await runInterceptors(finalRequest, this.interceptors),
        timeout: timedRequest,
        request: finalRequest,
      }
    } catch (error) {
      timedRequest.stop()
      throw error
    }
  }
}
