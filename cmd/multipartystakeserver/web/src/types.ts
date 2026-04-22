export interface ValidatorConfig {
  nodeID: string
  blsPublicKey: string
  blsProofOfPossession: string
  endTime: number
  delegationFee: number
}

export interface StakerConfig {
  address: string
  amount: number
  feePayer?: boolean
}

export interface InputConfig {
  validator: ValidatorConfig
  stakers: StakerConfig[]
}

export interface SignerInfo {
  address: string
}

export interface PrepareOutput {
  unsignedTx: unknown
  unsignedTxHex: string
  utxos: string[]
  signers: SignerInfo[]
}

export interface SplitPrepareOutput {
  unsignedTx: unknown
  unsignedTxHex: string
  utxos: string[]
  signers: SignerInfo[]
  validatorTxID: string
}

export type PartyState =
  | 'awaiting_validator_signatures'
  | 'validator_tx_issued'
  | 'awaiting_split_signatures'
  | 'awaiting_validation_end'
  | 'complete'

export interface CreatePartyResponse {
  partyID: string
  prepareOutput: PrepareOutput
  state: PartyState
  signaturesNeeded: string[]
  signaturesReceived: string[]
}

export interface GetPartyResponse {
  partyID: string
  state: PartyState
  prepareOutput: PrepareOutput
  signaturesNeeded: string[]
  signaturesReceived: string[]
  validatorTxID: string
  splitOutput: SplitPrepareOutput | null
  splitSignaturesReceived: string[]
  splitTxID: string
  potentialReward: number
  validatorEndTime?: number
}

export interface SignValidatorResponse {
  accepted: boolean
  state: PartyState
  signaturesReceived: string[]
  validatorTxID?: string
  potentialReward?: number
  splitOutput?: SplitPrepareOutput
}

export interface SignSplitResponse {
  accepted: boolean
  state: PartyState
  splitSignaturesReceived: string[]
  splitTxID?: string
}
