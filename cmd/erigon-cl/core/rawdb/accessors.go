package rawdb

import (
	"encoding/binary"
	"fmt"

	libcommon "github.com/gateway-fm/cdk-erigon-lib/common"
	"github.com/gateway-fm/cdk-erigon-lib/common/length"
	"github.com/gateway-fm/cdk-erigon-lib/kv"

	"github.com/ledgerwatch/erigon/cl/cltypes"
	"github.com/ledgerwatch/erigon/cl/utils"
	"github.com/ledgerwatch/erigon/cmd/erigon-cl/core/state"
)

func EncodeNumber(n uint64) []byte {
	ret := make([]byte, 4)
	binary.BigEndian.PutUint32(ret, uint32(n))
	return ret
}

// WriteBeaconState writes beacon state for specific block to database.
func WriteBeaconState(tx kv.Putter, state *state.BeaconState) error {
	data, err := utils.EncodeSSZSnappy(state)
	if err != nil {
		return err
	}

	return tx.Put(kv.BeaconState, EncodeNumber(state.Slot()), data)
}

// ReadBeaconState reads beacon state for specific block from database.
func ReadBeaconState(tx kv.Getter, slot uint64) (*state.BeaconState, error) {
	data, err := tx.GetOne(kv.BeaconState, EncodeNumber(slot))
	if err != nil {
		return nil, err
	}
	state := &state.BeaconState{}

	if len(data) == 0 {
		return nil, nil
	}

	if err := utils.DecodeSSZSnappy(state, data); err != nil {
		return nil, err
	}

	return state, nil
}

func WriteLightClientUpdate(tx kv.RwTx, update *cltypes.LightClientUpdate) error {
	key := make([]byte, 4)
	binary.BigEndian.PutUint32(key, uint32(update.SignatureSlot/8192))

	encoded, err := update.EncodeSSZ(nil)
	if err != nil {
		return err
	}
	return tx.Put(kv.LightClientUpdates, key, encoded)
}

func WriteLightClientFinalityUpdate(tx kv.RwTx, update *cltypes.LightClientFinalityUpdate) error {
	encoded, err := update.EncodeSSZ(nil)
	if err != nil {
		return err
	}
	return tx.Put(kv.LightClient, kv.LightClientFinalityUpdate, encoded)
}

func WriteLightClientOptimisticUpdate(tx kv.RwTx, update *cltypes.LightClientOptimisticUpdate) error {
	encoded, err := update.EncodeSSZ(nil)
	if err != nil {
		return err
	}
	return tx.Put(kv.LightClient, kv.LightClientOptimisticUpdate, encoded)
}

func ReadLightClientUpdate(tx kv.RwTx, period uint32) (*cltypes.LightClientUpdate, error) {
	key := make([]byte, 4)
	binary.BigEndian.PutUint32(key, period)

	encoded, err := tx.GetOne(kv.LightClientUpdates, key)
	if err != nil {
		return nil, err
	}
	update := &cltypes.LightClientUpdate{}
	if err = update.DecodeSSZ(encoded); err != nil {
		return nil, err
	}
	return update, nil
}

func ReadLightClientFinalityUpdate(tx kv.Tx) (*cltypes.LightClientFinalityUpdate, error) {
	encoded, err := tx.GetOne(kv.LightClient, kv.LightClientFinalityUpdate)
	if err != nil {
		return nil, err
	}
	if len(encoded) == 0 {
		return nil, nil
	}
	update := &cltypes.LightClientFinalityUpdate{}
	if err = update.DecodeSSZ(encoded); err != nil {
		return nil, err
	}
	return update, nil
}

func ReadLightClientOptimisticUpdate(tx kv.Tx) (*cltypes.LightClientOptimisticUpdate, error) {
	encoded, err := tx.GetOne(kv.LightClient, kv.LightClientOptimisticUpdate)
	if err != nil {
		return nil, err
	}
	if len(encoded) == 0 {
		return nil, nil
	}
	update := &cltypes.LightClientOptimisticUpdate{}
	if err = update.DecodeSSZ(encoded); err != nil {
		return nil, err
	}
	return update, nil
}

// Bytes2FromLength convert length to 2 bytes repressentation
func Bytes2FromLength(size int) []byte {
	return []byte{
		byte(size>>8) & 0xFF,
		byte(size>>0) & 0xFF,
	}
}

// LengthBytes2 convert length to 2 bytes repressentation
func LengthFromBytes2(buf []byte) int {
	return int(buf[0])*0x100 + int(buf[1])
}

func WriteAttestations(tx kv.RwTx, slot uint64, blockRoot libcommon.Hash, attestations []*cltypes.Attestation) error {
	return tx.Put(kv.Attestetations, append(EncodeNumber(slot), blockRoot[:]...), cltypes.EncodeAttestationsForStorage(attestations))
}

func ReadAttestations(tx kv.RwTx, blockRoot libcommon.Hash, slot uint64) ([]*cltypes.Attestation, error) {
	attestationsEncoded, err := tx.GetOne(kv.Attestetations, append(EncodeNumber(slot), blockRoot[:]...))
	if err != nil {
		return nil, err
	}
	return cltypes.DecodeAttestationsForStorage(attestationsEncoded)
}

func WriteBeaconBlock(tx kv.RwTx, signedBlock *cltypes.SignedBeaconBlock) error {
	var (
		block     = signedBlock.Block
		blockBody = block.Body
	)

	blockRoot, err := block.HashSSZ()
	if err != nil {
		return err
	}
	// database key is is [slot + block root]
	slotBytes := EncodeNumber(block.Slot)
	key := append(slotBytes, blockRoot[:]...)
	value, err := signedBlock.EncodeForStorage()
	if err != nil {
		return err
	}

	if err := WriteAttestations(tx, block.Slot, blockRoot, blockBody.Attestations); err != nil {
		return err
	}
	// Write block hashes
	// We write the block indexing
	if err := tx.Put(kv.RootSlotIndex, blockRoot[:], slotBytes); err != nil {
		return err
	}
	if err := tx.Put(kv.RootSlotIndex, block.StateRoot[:], key); err != nil {
		return err
	}
	// Finally write the beacon block
	return tx.Put(kv.BeaconBlocks, key, value)
}

func ReadBeaconBlock(tx kv.RwTx, blockRoot libcommon.Hash, slot uint64) (*cltypes.SignedBeaconBlock, uint64, libcommon.Hash, error) {
	signedBlock, eth1Number, eth1Hash, _, err := ReadBeaconBlockForStorage(tx, blockRoot, slot)
	if err != nil {
		return nil, 0, libcommon.Hash{}, err
	}
	if signedBlock == nil {
		return nil, 0, libcommon.Hash{}, err
	}

	attestations, err := ReadAttestations(tx, blockRoot, slot)
	if err != nil {
		return nil, 0, libcommon.Hash{}, err
	}
	signedBlock.Block.Body.Attestations = attestations
	return signedBlock, eth1Number, eth1Hash, err
}

func ReadBeaconBlockForStorage(tx kv.Getter, blockRoot libcommon.Hash, slot uint64) (block *cltypes.SignedBeaconBlock, eth1Number uint64, eth1Hash libcommon.Hash, eth2Hash libcommon.Hash, err error) {
	encodedBeaconBlock, err := tx.GetOne(kv.BeaconBlocks, append(EncodeNumber(slot), blockRoot[:]...))
	if err != nil {
		return nil, 0, libcommon.Hash{}, libcommon.Hash{}, err
	}
	if len(encodedBeaconBlock) == 0 {
		return nil, 0, libcommon.Hash{}, libcommon.Hash{}, nil
	}
	if len(encodedBeaconBlock) == 0 {
		return nil, 0, libcommon.Hash{}, libcommon.Hash{}, nil
	}
	return cltypes.DecodeBeaconBlockForStorage(encodedBeaconBlock)
}

func WriteFinalizedBlockRoot(tx kv.Putter, slot uint64, blockRoot libcommon.Hash) error {
	return tx.Put(kv.FinalizedBlockRoots, EncodeNumber(slot), blockRoot[:])
}

func ReadFinalizedBlockRoot(tx kv.Getter, slot uint64) (libcommon.Hash, error) {
	root, err := tx.GetOne(kv.FinalizedBlockRoots, EncodeNumber(slot))
	if err != nil {
		return libcommon.Hash{}, err
	}
	if len(root) == 0 {
		return libcommon.Hash{}, nil
	}
	if len(root) != length.Hash {
		return libcommon.Hash{}, fmt.Errorf("read block root with mismatching length")
	}
	return libcommon.BytesToHash(root), nil
}
