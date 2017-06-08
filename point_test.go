package bkdtree

import (
	"math/rand"
	"os"
	"testing"
)

type CaseInside struct {
	point, lowPoint, highPoint Point
	numDims                    int
	isInside                   bool
}

func TestIsInside(t *testing.T) {
	cases := []CaseInside{
		{
			Point{[]uint64{30, 80, 40}, 0},
			Point{[]uint64{30, 80, 40}, 0},
			Point{[]uint64{50, 90, 50}, 0},
			3,
			true,
		},
		{
			Point{[]uint64{30, 79, 40}, 0},
			Point{[]uint64{30, 80, 40}, 0},
			Point{[]uint64{50, 90, 50}, 0},
			3,
			false,
		},
		{ //invalid range
			Point{[]uint64{30, 80, 40}, 0},
			Point{[]uint64{30, 80, 40}, 0},
			Point{[]uint64{50, 90, 39}, 0},
			3,
			false,
		},
	}

	for i, tc := range cases {
		res := tc.point.Inside(tc.lowPoint, tc.highPoint)
		if res != tc.isInside {
			t.Fatalf("case %v failed\n", i)
		}
	}
}

func NewRandPoints(numDims int, maxVal uint64, size int) (points []Point) {
	for i := 0; i < size; i++ {
		vals := make([]uint64, 0, numDims)
		for j := 0; j < numDims; j++ {
			vals = append(vals, rand.Uint64()%maxVal)
		}
		point := Point{vals, uint64(i)}
		points = append(points, point)
	}
	return
}

func TestPointArrayExt_ToMem(t *testing.T) {
	numDims := 3
	maxVal := uint64(100)
	size := 10000
	points := NewRandPoints(numDims, maxVal, size)
	pam := PointArrayMem{
		points: points,
		byDim:  1,
	}

	tmpFp := "/tmp/point_test"
	tmpF, err := os.OpenFile(tmpFp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer tmpF.Close()

	bytesPerDim := 4
	pae, err := pam.ToExt(tmpF, bytesPerDim)
	if err != nil {
		t.Fatalf("%v", err)
	}
	pam2, err := pae.ToMem()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if pam.byDim != pam2.byDim {
		t.Fatalf("point array meta info changes after convertion")
	}
	if len(pam.points) != len(pam2.points) {
		t.Fatalf("point array length changes after convertion: %d %d", len(pam.points), len(pam2.points))
	}
	for i := 0; i < len(pam.points); i++ {
		p1, p2 := pam.points[i], pam2.points[i]
		if !p1.Equal(p2) {
			t.Fatalf("point content changes after convertion: %v %v", p1, p2)
		}
	}

}

//verify if lhs and rhs contains the same points. order doesn't matter.
func areSmaePoints(lhs, rhs []Point, numDims int) (res bool) {
	if len(lhs) != len(rhs) {
		return
	}
	numPoints := len(lhs)
	mapLhs := make(map[uint64]Point, numPoints)
	mapRhs := make(map[uint64]Point, numPoints)
	for i := 0; i < numPoints; i++ {
		mapLhs[lhs[i].UserData] = lhs[i]
		mapRhs[rhs[i].UserData] = rhs[i]
	}
	for k, v := range mapLhs {
		v2, found := mapRhs[k]
		if !found || !v.Equal(v2) {
			return
		}
	}
	return
}

func verifySplit(t *testing.T, pam *PointArrayMem, numStrips int, splitValues []uint64, splitPoses []int) {
	//fmt.Printf("points: %v\nsplitValues: %v\nsplitPoses:%v\n", points, splitValues, splitPoses)
	if len(splitValues) != numStrips-1 || len(splitValues) != len(splitPoses) {
		t.Fatalf("incorrect size of splitValues or splitPoses\n")
	}
	for i := 0; i < len(splitValues)-1; i++ {
		if splitValues[i] > splitValues[i+1] {
			t.Fatalf("incorrect splitValues\n")
		}
		if splitPoses[i] > splitPoses[i+1] {
			t.Fatalf("incorrect splitPoses\n")
		}
	}
	numSplits := len(splitValues)
	for strip := 0; strip < numStrips; strip++ {
		posBegin := 0
		minValue := uint64(0)
		if strip != 0 {
			posBegin = splitPoses[strip-1]
			minValue = splitValues[strip-1]
		}
		posEnd := len(pam.points)
		maxValue := ^uint64(0)
		if strip != numSplits {
			posEnd = splitPoses[strip]
			maxValue = splitValues[strip]
		}

		for pos := posBegin; pos < posEnd; pos++ {
			val := pam.points[pos].Vals[pam.byDim]
			if val < minValue || val > maxValue {
				t.Fatalf("points[%v] dim %v val %v is not in range %v-%v", pos, pam.byDim, val, minValue, maxValue)
			}
		}
	}
	return
}

func TestSplitPoints(t *testing.T) {
	//TODO: use suite setup to initialize points
	numDims := 3
	maxVal := uint64(100)
	size := 1000
	numStrips := 4
	points := NewRandPoints(numDims, maxVal, size)
	pointsSaved := make([]Point, size)
	copy(pointsSaved, points)
	//test SplitPoints(PointArrayMem)
	for dim := 0; dim < numDims; dim++ {
		pam := &PointArrayMem{
			points: points,
			byDim:  dim,
		}
		splitValues, splitPoses := SplitPoints(pam, numStrips)
		verifySplit(t, pam, numStrips, splitValues, splitPoses)
		if !areSmaePoints(pointsSaved, pam.points, numDims) {
			t.Fatalf("point set changes after split")
		}
	}

	//test SplitPoints(PointArrayExt)
	tmpFp := "/tmp/point_test"
	tmpF, err := os.OpenFile(tmpFp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer tmpF.Close()
	bytesPerDim := 4
	for dim := 0; dim < numDims; dim++ {
		pam := &PointArrayMem{
			points: points,
			byDim:  dim,
		}
		pae, err := pam.ToExt(tmpF, bytesPerDim)
		if err != nil {
			t.Fatalf("%v", err)
		}
		splitValues, splitPoses := SplitPoints(pae, numStrips)
		pam2, err := pae.ToMem()
		if err != nil {
			t.Fatalf("%v", err)
		}
		verifySplit(t, pam2, numStrips, splitValues, splitPoses)
		if !areSmaePoints(pam.points, pam2.points, numDims) {
			t.Fatalf("point set changes after split")
		}
	}
}
