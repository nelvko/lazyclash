// Package testfixture provides small, offline resources for the install
// integration tests. Neither fixture contains real IP or domain data.
package testfixture

// GeoSite holds the synthetic category CN with one full-domain entry.
func GeoSite() []byte {
	return []byte("\x0a\x15\x0a\x02CN\x12\x0f\x08\x03\x12\x0bexample.com")
}

// CountryMMDB is a synthetic Meta-geoip0 database. Both leaves of its only
// 24-bit search node point to the string "cn" after the 16-byte separator.
func CountryMMDB() []byte {
	data := []byte{0, 0, 17, 0, 0, 17}
	data = append(data, make([]byte, 16)...)
	data = append(data, "\x42cn\xab\xcd\xefMaxMind.com"...)
	return append(data, []byte("\xe8"+
		"\x5bbinary_format_major_version\xa1\x02"+
		"\x5bbinary_format_minor_version\xa0"+
		"\x4ddatabase_type\x4bMeta-geoip0"+
		"\x4bdescription\xe1\x42en\x47fixture"+
		"\x4aip_version\xa1\x04"+
		"\x4anode_count\xc1\x01"+
		"\x4brecord_size\xa1\x18"+
		"\x4abuild_epoch\x01\x02\x01")...)
}
