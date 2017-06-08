package bkdtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//Insert inserts given point. Fail if the tree is full.
func (bkd *BkdTree) Insert(point Point) (err error) {
	if bkd.NumPoints >= bkd.BkdCap {
		return errors.New("BKDTree is full")
	}
	//insert into in-memory buffer t0m. If t0m is not full, return.
	bkd.t0m = append(bkd.t0m, point)
	bkd.NumPoints++
	if len(bkd.t0m) < bkd.t0mCap {
		return
	}
	//find the smallest index k in [0, len(trees)) at which trees[k] is empty, or its capacity is no less than the sum of size of t0m + trees[0:k+1]
	sum := len(bkd.t0m)
	var k int
	for k = 0; k < len(bkd.trees); k++ {
		if bkd.trees[k].numPoints == 0 {
			break
		}
		sum += int(bkd.trees[k].numPoints)
		capK := bkd.t0mCap << uint(k)
		if capK >= sum {
			break
		}
	}
	if k == len(bkd.trees) {
		kd := KdTreeExtMeta{
			pointsOffEnd: 0,
			rootOff:      0,
			numPoints:    0,
			leafCap:      uint16(bkd.leafCap),
			intraCap:     uint16(bkd.intraCap),
			numDims:      uint8(bkd.numDims),
			bytesPerDim:  uint8(bkd.bytesPerDim),
			pointSize:    uint8(bkd.pointSize),
			formatVer:    0,
		}
		bkd.trees = append(bkd.trees, kd)
	}
	//extract all points from t0m and trees[0:k+1] into a file F
	tmpFpK := filepath.Join(bkd.dir, fmt.Sprintf("%s_%d.tmp", bkd.prefix, k))
	fpK := filepath.Join(bkd.dir, fmt.Sprintf("%s_%d", bkd.prefix, k))
	tmpFK, err := os.OpenFile(tmpFpK, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return
	}

	err = bkd.extractT0M(tmpFK)
	if err != nil {
		return
	}
	for i := 0; i <= k; i++ {
		err = bkd.extractTi(tmpFK, i)
		if err != nil {
			return
		}
	}
	meta, err := bkd.bulkLoad(tmpFK)
	if err != nil {
		return
	}

	//empty T0M and Ti, 0<=i<k
	bkd.t0m = make([]Point, 0, bkd.t0mCap)
	for i := 0; i <= k; i++ {
		if bkd.trees[i].numPoints <= 0 {
			continue
		}
		fpI := filepath.Join(bkd.dir, fmt.Sprintf("%s_%d", bkd.prefix, i))
		err = os.Remove(fpI)
		if err != nil {
			return
		}
		bkd.trees[i].numPoints = 0
	}
	err = os.Rename(tmpFpK, fpK) //TODO: what happen if tmpF is open?
	if err != nil {
		return
	}
	bkd.trees[k] = *meta
	return
}

func (bkd *BkdTree) extractT0M(tmpF *os.File) (err error) {
	for _, point := range bkd.t0m {
		bytesP := point.Encode(bkd.bytesPerDim)
		_, err = tmpF.Write(bytesP)
		if err != nil {
			return
		}
	}
	return
}

func (bkd *BkdTree) extractTi(dstF *os.File, idx int) (err error) {
	if bkd.trees[idx].numPoints <= 0 {
		return
	}
	fp := filepath.Join(bkd.dir, fmt.Sprintf("%s_%d", bkd.prefix, idx))
	srcF, err := os.Open(fp)
	if err != nil {
		return
	}
	defer srcF.Close()

	//depth-first extracting from the root node
	meta := &bkd.trees[idx]
	err = bkd.extractNode(dstF, srcF, meta, int64(meta.rootOff))
	return
}

func (bkd *BkdTree) extractNode(dstF, srcF *os.File, meta *KdTreeExtMeta, nodeOffset int64) (err error) {
	if nodeOffset < 0 {
		err = fmt.Errorf("invalid nodeOffset %d", nodeOffset)
		return
	}
	_, err = srcF.Seek(nodeOffset, 0)
	if err != nil {
		return
	}
	var node KdTreeExtIntraNode
	err = node.Read(srcF)
	if err != nil {
		return
	}
	for _, child := range node.Children {
		if child.Offset < meta.pointsOffEnd {
			//leaf node
			//TODO: use Linux syscall.Splice() instead?
			_, err = srcF.Seek(int64(child.Offset), 0)
			if err != nil {
				return
			}
			length := int64(child.NumPoints) * int64(meta.pointSize)
			_, err = io.CopyN(dstF, srcF, length)
			if err != nil {
				return
			}
		} else {
			//intra node
			err = bkd.extractNode(dstF, srcF, meta, int64(child.Offset))
			if err != nil {
				return
			}
		}
	}
	return
}

func (bkd *BkdTree) bulkLoad(tmpF *os.File) (meta *KdTreeExtMeta, err error) {
	pointsOffEnd, err := tmpF.Seek(0, 1) //get current position
	if err != nil {
		return
	}
	numPoints := int(pointsOffEnd / int64(bkd.pointSize))
	rootOff, err1 := bkd.createKdTreeExt(tmpF, 0, numPoints, 0)
	if err1 != nil {
		err = err1
		return
	}
	//record meta info at end
	meta = &KdTreeExtMeta{
		pointsOffEnd: uint64(pointsOffEnd),
		rootOff:      uint64(rootOff),
		numPoints:    uint64(numPoints),
		leafCap:      uint16(bkd.leafCap),
		intraCap:     uint16(bkd.intraCap),
		numDims:      uint8(bkd.numDims),
		bytesPerDim:  uint8(bkd.bytesPerDim),
		pointSize:    uint8(bkd.pointSize),
		formatVer:    0,
	}
	err = binary.Write(tmpF, binary.BigEndian, meta)
	if err != nil {
		return
	}
	err = tmpF.Close()
	return
}

func getCurrentOffset(f *os.File) (offset int64, err error) {
	offset, err = f.Seek(0, 1) //get current position
	return
}

func (bkd *BkdTree) createKdTreeExt(tmpF *os.File, begin, end, depth int) (offset int64, err error) {
	if begin >= end {
		err = fmt.Errorf("assertion begin>=end failed, begin %v, end %v", begin, end)
		return
	}

	splitDim := depth % bkd.numDims
	numStrips := (end - begin + bkd.leafCap - 1) / bkd.leafCap
	if numStrips > bkd.intraCap {
		numStrips = bkd.intraCap
	}

	pae := PointArrayExt{
		f:           tmpF,
		offBegin:    int64(begin * bkd.pointSize),
		numPoints:   end - begin,
		byDim:       splitDim,
		bytesPerDim: bkd.bytesPerDim,
		numDims:     bkd.numDims,
		pointSize:   bkd.pointSize,
	}
	splitValues, splitPoses := SplitPoints(&pae, numStrips)

	children := make([]KdTreeExtNodeInfo, 0, numStrips)
	var childOffset int64
	for strip := 0; strip < numStrips; strip++ {
		posBegin := begin
		if strip != 0 {
			posBegin = begin + splitPoses[strip-1]
		}
		posEnd := end
		if strip != numStrips-1 {
			posEnd = begin + splitPoses[strip]
		}
		if posEnd-posBegin <= bkd.leafCap {
			info := KdTreeExtNodeInfo{
				Offset:    uint64(posBegin * bkd.pointSize),
				NumPoints: uint64(posEnd - posBegin),
			}
			children = append(children, info)
		} else {
			childOffset, err = bkd.createKdTreeExt(tmpF, posBegin, posEnd, depth+1)
			if err != nil {
				return
			}
			info := KdTreeExtNodeInfo{
				Offset:    uint64(childOffset),
				NumPoints: uint64(posEnd - posBegin),
			}
			children = append(children, info)
		}
	}

	offset, err = getCurrentOffset(tmpF)
	if err != nil {
		return
	}

	node := &KdTreeExtIntraNode{
		SplitDim:    uint32(splitDim),
		NumStrips:   uint32(numStrips),
		SplitValues: splitValues,
		Children:    children,
	}
	err = node.Write(tmpF)
	return
}
