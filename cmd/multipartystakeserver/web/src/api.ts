import type {
  InputConfig,
  CreatePartyResponse,
  GetPartyResponse,
  SignValidatorResponse,
  SignSplitResponse,
} from './types'

const BASE = '/api'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  const body = await res.json()
  if (!res.ok) {
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  return body as T
}

export function createParty(config: InputConfig): Promise<CreatePartyResponse> {
  return request('/parties', {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

export function getParty(id: string): Promise<GetPartyResponse> {
  return request(`/parties/${id}`)
}

// Core Wallet path: signs via wallet, sends signedTxHex
export function signValidatorWithWallet(
  id: string,
  address: string,
  signedTxHex: string,
): Promise<SignValidatorResponse> {
  return request(`/parties/${id}/sign-validator`, {
    method: 'POST',
    body: JSON.stringify({ address, signedTxHex }),
    signal: AbortSignal.timeout(60_000),
  })
}

// Dev path: server signs with the provided private key
export function signValidatorWithKey(
  id: string,
  privateKeyCB58: string,
): Promise<SignValidatorResponse> {
  return request(`/parties/${id}/sign-validator`, {
    method: 'POST',
    body: JSON.stringify({ privateKeyCB58 }),
    signal: AbortSignal.timeout(60_000),
  })
}

export function signSplitWithWallet(
  id: string,
  address: string,
  signedTxHex: string,
): Promise<SignSplitResponse> {
  return request(`/parties/${id}/sign-split`, {
    method: 'POST',
    body: JSON.stringify({ address, signedTxHex }),
    signal: AbortSignal.timeout(60_000),
  })
}

export function signSplitWithKey(
  id: string,
  privateKeyCB58: string,
): Promise<SignSplitResponse> {
  return request(`/parties/${id}/sign-split`, {
    method: 'POST',
    body: JSON.stringify({ privateKeyCB58 }),
    signal: AbortSignal.timeout(60_000),
  })
}

// Dev: derive P-chain address from a CB58 private key
export function deriveAddress(
  privateKeyCB58: string,
  networkID: number,
): Promise<{ address: string }> {
  return request('/dev/derive-address', {
    method: 'POST',
    body: JSON.stringify({ privateKeyCB58, networkID }),
  })
}
