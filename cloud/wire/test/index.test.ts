import { describe, it, expect } from 'vitest'
import {
  PROTOCOL_VERSION,
  OperationalHeader,
  WireMessage,
  AgentEvent,
  RpcRequest,
  BackendCapabilitiesLite,
  RunSummary,
  MachineInfo,
  ClientWsMessage,
  ServerWsMessage,
  PairInit,
  PairComplete,
  PairStatus,
} from '../src/index'

describe('wire index', () => {
  it('exposes PROTOCOL_VERSION = 1', () => {
    expect(PROTOCOL_VERSION).toBe(1)
  })

  it('re-exports the core schemas', () => {
    expect(typeof OperationalHeader.parse).toBe('function')
    expect(typeof WireMessage.parse).toBe('function')
    expect(typeof AgentEvent.parse).toBe('function')
    expect(typeof RpcRequest.parse).toBe('function')
  })

  it('re-exports the batch-1 spine schemas', () => {
    expect(typeof BackendCapabilitiesLite.parse).toBe('function')
    expect(typeof RunSummary.parse).toBe('function')
    expect(typeof MachineInfo.parse).toBe('function')
    expect(typeof ClientWsMessage.parse).toBe('function')
    expect(typeof ServerWsMessage.parse).toBe('function')
    expect(typeof PairInit.parse).toBe('function')
    expect(typeof PairComplete.parse).toBe('function')
    expect(typeof PairStatus.parse).toBe('function')
  })
})
