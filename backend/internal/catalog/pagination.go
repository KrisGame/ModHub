package catalog

type SubQueryPageStat struct {
	Fetched int
	Total   int64
}

type PlatformPagination struct {
	Total      int64
	NextOffset int
	HasMore    bool
}

func ComputeSearchPagination(curOffset, fetchLim, uniqueFetched int, subs []SubQueryPageStat) PlatformPagination {
	if len(subs) == 0 {
		return PlatformPagination{Total: int64(uniqueFetched), NextOffset: curOffset, HasMore: false}
	}
	if len(subs) == 1 {
		s := subs[0]
		tot := s.Total
		if int64(uniqueFetched) > tot {
			tot = int64(uniqueFetched)
		}
		next := curOffset + s.Fetched
		hasMore := s.Fetched > 0 && int64(next) < tot
		return PlatformPagination{Total: tot, NextOffset: next, HasMore: hasMore}
	}
	var maxTotal, sumTotal int64
	var sumFetched, maxFetched int
	anyMore := false
	for _, s := range subs {
		if s.Total > maxTotal {
			maxTotal = s.Total
		}
		sumTotal += s.Total
		sumFetched += s.Fetched
		if s.Fetched > maxFetched {
			maxFetched = s.Fetched
		}
		if s.Fetched > 0 && int64(curOffset+s.Fetched) < s.Total {
			anyMore = true
		}
	}
	estTotal := maxTotal
	if sumFetched > 0 && uniqueFetched > 0 {
		ratio := float64(uniqueFetched) / float64(sumFetched)
		scaled := int64(float64(sumTotal) * ratio)
		if scaled > estTotal {
			estTotal = scaled
		}
	}
	if int64(curOffset+uniqueFetched) > estTotal {
		estTotal = int64(curOffset + uniqueFetched)
	}
	step := maxFetched
	if anyMore && fetchLim > 0 && step > fetchLim {
		step = fetchLim
	}
	next := curOffset + step
	hasMore := anyMore && step > 0 && int64(next) < estTotal
	return PlatformPagination{Total: estTotal, NextOffset: next, HasMore: hasMore}
}
