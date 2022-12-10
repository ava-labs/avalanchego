package state

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	db_manager "github.com/ava-labs/avalanchego/database/manager"
)

func TestGetDeposit(t *testing.T) {
	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)
	addr0 := ids.GenerateTestShortID()
	addresses := set.NewSet[ids.ShortID](0)
	addresses.Add(addr0)
	testID := ids.GenerateTestID()
	deposit1 := deposit.Deposit{
		DepositOfferID: testID,
		Start:          uint64(time.Now().Unix()),
		Duration:       uint32((10 * time.Minute).Seconds()),
		Amount:         1,
	}
	type args struct {
		depositTxID ids.ID
	}
	tests := map[string]struct {
		cs          CaminoState
		args        args
		prepareFunc func(cs CaminoState)
		want        *deposit.Deposit
		err         error
	}{
		"retrieve from modified state": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				cs.UpdateDeposit(testID, &deposit1)
			},
			want: &deposit1,
		},
		"retrieve from modified state when deposit already in db": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				cs.UpdateDeposit(testID, &deposit1)

				db := cs.(*caminoState).depositsDB
				depositBytes, _ := blocks.GenesisCodec.Marshal(blocks.Version, deposit.Deposit{DepositOfferID: testID, Amount: deposit1.Amount + 1})
				db.Put(testID[:], depositBytes) //nolint:errcheck
			},
			want: &deposit1,
		},
		"retrieval fails when deposit already in modified state but with nil value even if present in db": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				cs.UpdateDeposit(testID, nil)

				db := cs.(*caminoState).depositsDB
				depositBytes, _ := blocks.GenesisCodec.Marshal(blocks.Version, &deposit1)
				db.Put(testID[:], depositBytes) //nolint:errcheck
			},
			err: database.ErrNotFound,
		},
		"retrieve from cache": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				cache := cs.(*caminoState).depositsCache
				cache.Put(testID, &deposit1)
			},
			want: &deposit1,
		},
		"retrieve from cache when deposit already in db": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				cache := cs.(*caminoState).depositsCache
				cache.Put(testID, &deposit1)

				db := cs.(*caminoState).depositsDB
				depositBytes, _ := blocks.GenesisCodec.Marshal(blocks.Version, deposit.Deposit{DepositOfferID: testID, Amount: deposit1.Amount + 1})
				db.Put(testID[:], depositBytes) //nolint:errcheck
			},
			want: &deposit1,
		},
		"retrieve from db": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {
				db := cs.(*caminoState).depositsDB
				depositBytes, _ := blocks.GenesisCodec.Marshal(blocks.Version, deposit1)
				db.Put(testID[:], depositBytes) //nolint:errcheck
			},
			want: &deposit1,
		},
		"db retrieval failed": {
			cs: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				return cs
			}(),
			args: args{
				depositTxID: testID,
			},
			prepareFunc: func(cs CaminoState) {},
			err:         database.ErrNotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.prepareFunc(tt.cs)
			got, err := tt.cs.GetDeposit(tt.args.depositTxID)
			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWriteDeposits(t *testing.T) {
	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)
	addr0 := ids.GenerateTestShortID()
	addresses := set.NewSet[ids.ShortID](0)
	addresses.Add(addr0)
	testID := ids.GenerateTestID()

	deposit1 := deposit.Deposit{
		DepositOfferID: testID,
		Start:          uint64(time.Now().Unix()),
		Duration:       uint32((10 * time.Minute).Seconds()),
		Amount:         1,
	}
	tests := map[string]struct {
		prepareFunc            func() CaminoState
		wantedModifiedDeposits map[ids.ID]*deposit.Deposit
		wantedDepositsDB       database.Database
		err                    error
	}{
		"Deposit nil / success db deletion": {
			prepareFunc: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				cs.modifiedDeposits[testID] = nil
				return cs
			},
			wantedModifiedDeposits: map[ids.ID]*deposit.Deposit{},
		},
		"Deposit nil / error in db deletion": {
			prepareFunc: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				cs.modifiedDeposits[testID] = nil
				cs.depositsDB.Close() // close db connection to cause error on deletion
				return cs
			},
			err: database.ErrClosed,
		},
		"Success": {
			prepareFunc: func() CaminoState {
				cs, _ := newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())
				cs.modifiedDeposits[testID] = &deposit1
				return cs
			},
			wantedModifiedDeposits: map[ids.ID]*deposit.Deposit{},
			wantedDepositsDB:       versiondb.New(baseDBManager.Current().Database),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cs := tt.prepareFunc()

			err := cs.(*caminoState).writeDeposits()
			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantedModifiedDeposits, cs.(*caminoState).modifiedDeposits)
		})
	}
}
