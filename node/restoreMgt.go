package node

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CESSProject/cess-bucket/pkg/utils"
	"github.com/CESSProject/cess-go-sdk/core/erasure"
	"github.com/CESSProject/cess-go-sdk/core/pattern"
	sutils "github.com/CESSProject/cess-go-sdk/core/utils"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
)

func (n *Node) restoreMgt(ch chan bool) {
	defer func() {
		ch <- true
		if err := recover(); err != nil {
			n.Pnc(utils.RecoverError(err))
		}
	}()

	n.Restore("info", ">>>>> Start restoreMgt")
	for {
		for n.GetChainState() {
			err := n.inspector()
			if err != nil {
				n.Restore("err", err.Error())
				time.Sleep(pattern.BlockInterval)
			}
			err = n.claimRestoreOrder()
			if err != nil {
				n.Restore("err", err.Error())
				time.Sleep(pattern.BlockInterval)
			}
		}
		time.Sleep(pattern.BlockInterval)
	}
}

func (n *Node) inspector() error {
	var (
		err      error
		roothash string
		fmeta    pattern.FileMetadata
	)

	roothashes, err := utils.Dirs(n.GetDirs().FileDir)
	if err != nil {
		n.Restore("err", fmt.Sprintf("[Dir %v] %v", n.GetDirs().FileDir, err))
		roothashes, err = n.QueryPrefixKeyList(Cach_prefix_metadata)
		if err != nil {
			return errors.Wrapf(err, "[QueryPrefixKeyList]")
		}
	}

	for _, v := range roothashes {
		roothash = filepath.Base(v)
		fmeta, err = n.QueryFileMetadata(v)
		if err != nil {
			if err.Error() == pattern.ERR_Empty {
				os.RemoveAll(v)
				continue
			}
			n.Restore("err", fmt.Sprintf("[QueryFileMetadata %v] %v", roothash, err))
			continue
		}
		for _, segment := range fmeta.SegmentList {
			for _, fragment := range segment.FragmentList {
				if sutils.CompareSlice(fragment.Miner[:], n.GetStakingPublickey()) {
					_, err = os.Stat(filepath.Join(n.GetDirs().FileDir, roothash, string(fragment.Hash[:])))
					if err != nil {
						err = n.restoreFragment(roothashes, roothash, string(fragment.Hash[:]), segment)
						if err != nil {
							n.Restore("err", fmt.Sprintf("[restoreFragment %v] %v", roothash, err))
							_, err = n.GenerateRestoralOrder(roothash, string(fragment.Hash[:]))
							if err != nil {
								n.Restore("err", fmt.Sprintf("[GenerateRestoralOrder %v] %v", roothash, err))
							}
							continue
						}
					}
				}
			}
		}
	}

	return nil
}

func (n *Node) restoreFragment(roothashes []string, roothash, framentHash string, segement pattern.SegmentInfo) error {
	var err error
	var id peer.ID
	var miner pattern.MinerInfo
	for _, v := range roothashes {
		_, err = os.Stat(filepath.Join(v, framentHash))
		if err == nil {
			err = utils.CopyFile(filepath.Join(n.GetDirs().FileDir, roothash, framentHash), filepath.Join(v, framentHash))
			if err == nil {
				return nil
			}
		}
	}
	var canRestore int
	var recoverList []string
	for _, v := range segement.FragmentList {
		if string(v.Hash[:]) == framentHash {
			continue
		}
		miner, err = n.QueryStorageMiner(v.Miner[:])
		if err != nil {
			continue
		}
		id, err = peer.Decode(base58.Encode([]byte(string(miner.PeerId[:]))))
		if err != nil {
			continue
		}
		err = n.ReadFileAction(id, roothash, framentHash, filepath.Join(n.GetDirs().FileDir, roothash, framentHash), pattern.FragmentSize)
		if err != nil {
			n.Restore("err", fmt.Sprintf("[ReadFileAction] %v", err))
			continue
		}
		recoverList = append(recoverList, filepath.Join(n.GetDirs().FileDir, roothash, framentHash))
		canRestore++
		if canRestore >= int(len(segement.FragmentList)*2/3) {
			break
		}
	}
	segmentpath := filepath.Join(n.GetDirs().TmpDir, roothash, string(segement.Hash[:]))
	if canRestore >= int(len(segement.FragmentList)*2/3) {
		err = n.RedundancyRecovery(segmentpath, recoverList)
		if err != nil {
			os.Remove(segmentpath)
			return err
		}
		_, err = erasure.ReedSolomon(segmentpath)
		if err != nil {
			return err
		}
		_, err = os.Stat(filepath.Join(n.GetDirs().FileDir, roothash, framentHash))
		if err != nil {
			return errors.New("recpvery failed")
		}
	} else {
		return errors.New("recpvery failed")
	}

	return nil
}

func (n *Node) claimRestoreOrder() error {
	val, _ := n.QueryPrefixKeyList(Cach_prefix_recovery)
	for _, v := range val {
		b, err := n.Get([]byte(Cach_prefix_recovery + v))
		if err != nil {
			n.Restore("err", fmt.Sprintf("[Get %s] %v", v, err))
			n.Delete([]byte(Cach_prefix_recovery + v))
			continue
		}
		err = n.restoreAFragment(string(b), v, filepath.Join(n.GetDirs().FileDir, string(b), v))
		if err != nil {
			n.Restore("err", fmt.Sprintf("[restoreAFragment %s-%s] %v", string(b), v, err))
			continue
		}
		txhash, err := n.RestoralComplete(v)
		if err != nil {
			n.Restore("err", fmt.Sprintf("[RestoralComplete %s-%s] %v", string(b), v, err))
			continue
		}
		n.Restore("info", fmt.Sprintf("[RestoralComplete %s-%s] %s", string(b), v, txhash))
	}

	restoreOrderList, err := n.QueryRestoralOrderList()
	if err != nil {
		n.Restore("err", fmt.Sprintf("[QueryRestoralOrderList] %v", err))
		return err
	}
	blockHeight, err := n.QueryBlockHeight("")
	if err != nil {
		n.Restore("err", fmt.Sprintf("[QueryBlockHeight] %v", err))
		return err
	}
	for _, v := range restoreOrderList {
		if blockHeight <= uint32(v.Deadline) {
			continue
		}
		_, err = n.ClaimRestoralOrder(string(v.FragmentHash[:]))
		if err != nil {
			n.Restore("err", fmt.Sprintf("[ClaimRestoralOrder] %v", err))
			continue
		}
		n.Put([]byte(Cach_prefix_recovery+string(v.FragmentHash[:])), []byte(string(v.FileHash[:])))
		break
	}

	return nil
}

func (n *Node) restoreAFragment(roothash, framentHash, recoveryPath string) error {
	var err error
	var id peer.ID
	var miner pattern.MinerInfo
	roothashes, err := utils.Dirs(n.GetDirs().FileDir)
	for _, v := range roothashes {
		_, err = os.Stat(filepath.Join(v, framentHash))
		if err == nil {
			err = utils.CopyFile(recoveryPath, filepath.Join(v, framentHash))
			if err == nil {
				return nil
			}
		}
	}
	var canRestore int
	var recoverList []string
	var dstSegement pattern.SegmentInfo
	fmeta, err := n.QueryFileMetadata(roothash)
	if err != nil {
		return err
	}
	for _, segement := range fmeta.SegmentList {
		for _, v := range segement.FragmentList {
			if string(v.Hash[:]) == framentHash {
				dstSegement = segement
				break
			}
		}
		if dstSegement.FragmentList != nil {
			break
		}
	}

	for _, v := range dstSegement.FragmentList {
		if string(v.Hash[:]) == framentHash {
			continue
		}
		miner, err = n.QueryStorageMiner(v.Miner[:])
		if err != nil {
			continue
		}
		id, err = peer.Decode(base58.Encode([]byte(string(miner.PeerId[:]))))
		if err != nil {
			continue
		}
		err = n.ReadFileAction(id, roothash, framentHash, filepath.Join(n.GetDirs().FileDir, roothash, framentHash), pattern.FragmentSize)
		if err != nil {
			n.Restore("err", fmt.Sprintf("[ReadFileAction] %v", err))
			continue
		}
		recoverList = append(recoverList, filepath.Join(n.GetDirs().FileDir, roothash, framentHash))
		canRestore++
		if canRestore >= int(len(dstSegement.FragmentList)*2/3) {
			break
		}
	}

	segmentpath := filepath.Join(n.GetDirs().TmpDir, roothash, string(dstSegement.Hash[:]))
	if canRestore >= int(len(dstSegement.FragmentList)*2/3) {
		err = n.RedundancyRecovery(segmentpath, recoverList)
		if err != nil {
			os.Remove(segmentpath)
			return err
		}
		_, err = erasure.ReedSolomon(segmentpath)
		if err != nil {
			return err
		}
		_, err = os.Stat(filepath.Join(n.GetDirs().FileDir, roothash, framentHash))
		if err != nil {
			return errors.New("recpvery failed")
		}
	} else {
		return errors.New("recpvery failed")
	}

	return nil
}

func (n *Node) fetchFile(roothash, fragmentHash, path string) bool {
	var err error
	var ok bool
	var id peer.ID
	peers := n.GetAllTeePeerId()

	for _, v := range peers {
		id, err = peer.Decode(v)
		if err != nil {
			continue
		}
		err = n.ReadFileAction(id, roothash, fragmentHash, path, pattern.FragmentSize)
		if err != nil {
			continue
		}
		ok = true
		break
	}
	return ok
}
