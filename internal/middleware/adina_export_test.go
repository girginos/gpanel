package middleware

// AdinaKararKaynak: dış test paketi (middleware_test) için — kendi domaini askıdaki
// bayinin verilen askı kaynağındaki kararı (hesapaski sabitleriyle eşitlik testi).
func AdinaKararKaynak(rid int64, kaynak string) (int, string) {
	return AdinaKarar("reseller", rid, adinaDomain{bulundu: true, resellerID: rid, askida: true, kaynak: kaynak})
}
