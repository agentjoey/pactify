import { createAdapter } from '@socket.io/redis-adapter'
import { createClient } from 'redis'

export type RedisAdapterConstructor = ReturnType<typeof createAdapter>

function logRedisError(err: Error) {
  console.error('redis adapter client error:', err)
}

/**
 * Create a socket.io Redis adapter when a Redis URL is configured.
 *
 * The adapter lets multiple relay instances route messages to any connected
 * socket via Redis pub/sub. When no URL is provided the relay stays
 * single-instance and adapter-less, keeping tests hermetic.
 */
export async function maybeMakeAdapter(
  redisUrl?: string,
): Promise<RedisAdapterConstructor | undefined> {
  if (!redisUrl) return undefined
  const pub = createClient({ url: redisUrl })
  const sub = pub.duplicate()
  pub.on('error', logRedisError)
  sub.on('error', logRedisError)
  await Promise.all([pub.connect(), sub.connect()])
  return createAdapter(pub, sub)
}
