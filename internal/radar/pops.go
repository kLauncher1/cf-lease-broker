package radar

import "strings"

var defaultPoPs = []PoPInfo{
	// North America
	{IATA: "IAD", City: "Ashburn", Country: "US", Region: "North America", Location: GeoPoint{Lat: 39.0438, Lon: -77.4874}, ProbeEndpoint: "https://iad.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "SFO", City: "San Francisco", Country: "US", Region: "North America", Location: GeoPoint{Lat: 37.6213, Lon: -122.3790}, ProbeEndpoint: "https://sfo.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "ORD", City: "Chicago", Country: "US", Region: "North America", Location: GeoPoint{Lat: 41.9742, Lon: -87.9073}, ProbeEndpoint: "https://ord.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "DFW", City: "Dallas", Country: "US", Region: "North America", Location: GeoPoint{Lat: 32.8998, Lon: -97.0403}, ProbeEndpoint: "https://dfw.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "ATL", City: "Atlanta", Country: "US", Region: "North America", Location: GeoPoint{Lat: 33.6407, Lon: -84.4277}, ProbeEndpoint: "https://atl.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "SEA", City: "Seattle", Country: "US", Region: "North America", Location: GeoPoint{Lat: 47.4502, Lon: -122.3088}, ProbeEndpoint: "https://sea.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "LAX", City: "Los Angeles", Country: "US", Region: "North America", Location: GeoPoint{Lat: 33.9416, Lon: -118.4085}, ProbeEndpoint: "https://lax.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "MIA", City: "Miami", Country: "US", Region: "North America", Location: GeoPoint{Lat: 25.7959, Lon: -80.2870}, ProbeEndpoint: "https://mia.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "YYZ", City: "Toronto", Country: "CA", Region: "North America", Location: GeoPoint{Lat: 43.6777, Lon: -79.6248}, ProbeEndpoint: "https://yyz.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "YVR", City: "Vancouver", Country: "CA", Region: "North America", Location: GeoPoint{Lat: 49.1967, Lon: -123.1815}, ProbeEndpoint: "https://yvr.cloudflare.com/cdn-cgi/trace", Tier: 2},

	// Europe
	{IATA: "LHR", City: "London", Country: "GB", Region: "Europe", Location: GeoPoint{Lat: 51.4700, Lon: -0.4543}, ProbeEndpoint: "https://lhr.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "FRA", City: "Frankfurt", Country: "DE", Region: "Europe", Location: GeoPoint{Lat: 50.0379, Lon: 8.5622}, ProbeEndpoint: "https://fra.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "AMS", City: "Amsterdam", Country: "NL", Region: "Europe", Location: GeoPoint{Lat: 52.3105, Lon: 4.7683}, ProbeEndpoint: "https://ams.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "CDG", City: "Paris", Country: "FR", Region: "Europe", Location: GeoPoint{Lat: 49.0097, Lon: 2.5479}, ProbeEndpoint: "https://cdg.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "MAD", City: "Madrid", Country: "ES", Region: "Europe", Location: GeoPoint{Lat: 40.4839, Lon: -3.5680}, ProbeEndpoint: "https://mad.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "ARN", City: "Stockholm", Country: "SE", Region: "Europe", Location: GeoPoint{Lat: 59.6498, Lon: 17.9238}, ProbeEndpoint: "https://arn.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "DUB", City: "Dublin", Country: "IE", Region: "Europe", Location: GeoPoint{Lat: 53.4264, Lon: -6.2499}, ProbeEndpoint: "https://dub.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "MXP", City: "Milan", Country: "IT", Region: "Europe", Location: GeoPoint{Lat: 45.6301, Lon: 8.7255}, ProbeEndpoint: "https://mxp.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "ZRH", City: "Zurich", Country: "CH", Region: "Europe", Location: GeoPoint{Lat: 47.4582, Lon: 8.5555}, ProbeEndpoint: "https://zrh.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "WAW", City: "Warsaw", Country: "PL", Region: "Europe", Location: GeoPoint{Lat: 52.1672, Lon: 20.9679}, ProbeEndpoint: "https://waw.cloudflare.com/cdn-cgi/trace", Tier: 2},

	// Asia Pacific
	{IATA: "ICN", City: "Seoul", Country: "KR", Region: "Asia Pacific", Location: GeoPoint{Lat: 37.4602, Lon: 126.4407}, ProbeEndpoint: "https://icn.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "NRT", City: "Tokyo", Country: "JP", Region: "Asia Pacific", Location: GeoPoint{Lat: 35.7720, Lon: 140.3929}, ProbeEndpoint: "https://nrt.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "KIX", City: "Osaka", Country: "JP", Region: "Asia Pacific", Location: GeoPoint{Lat: 34.4320, Lon: 135.2304}, ProbeEndpoint: "https://kix.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "SIN", City: "Singapore", Country: "SG", Region: "Asia Pacific", Location: GeoPoint{Lat: 1.3644, Lon: 103.9915}, ProbeEndpoint: "https://sin.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "HKG", City: "Hong Kong", Country: "HK", Region: "Asia Pacific", Location: GeoPoint{Lat: 22.3080, Lon: 113.9185}, ProbeEndpoint: "https://hkg.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "TPE", City: "Taipei", Country: "TW", Region: "Asia Pacific", Location: GeoPoint{Lat: 25.0797, Lon: 121.2342}, ProbeEndpoint: "https://tpe.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "SYD", City: "Sydney", Country: "AU", Region: "Asia Pacific", Location: GeoPoint{Lat: -33.9399, Lon: 151.1753}, ProbeEndpoint: "https://syd.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "MEL", City: "Melbourne", Country: "AU", Region: "Asia Pacific", Location: GeoPoint{Lat: -37.6690, Lon: 144.8410}, ProbeEndpoint: "https://mel.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "BOM", City: "Mumbai", Country: "IN", Region: "Asia Pacific", Location: GeoPoint{Lat: 19.0896, Lon: 72.8656}, ProbeEndpoint: "https://bom.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "BKK", City: "Bangkok", Country: "TH", Region: "Asia Pacific", Location: GeoPoint{Lat: 13.6900, Lon: 100.7501}, ProbeEndpoint: "https://bkk.cloudflare.com/cdn-cgi/trace", Tier: 2},

	// Latin America
	{IATA: "GRU", City: "Sao Paulo", Country: "BR", Region: "Latin America", Location: GeoPoint{Lat: -23.4356, Lon: -46.4731}, ProbeEndpoint: "https://gru.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "GIG", City: "Rio de Janeiro", Country: "BR", Region: "Latin America", Location: GeoPoint{Lat: -22.8134, Lon: -43.2494}, ProbeEndpoint: "https://gig.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "EZE", City: "Buenos Aires", Country: "AR", Region: "Latin America", Location: GeoPoint{Lat: -34.8222, Lon: -58.5358}, ProbeEndpoint: "https://eze.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "SCL", City: "Santiago", Country: "CL", Region: "Latin America", Location: GeoPoint{Lat: -33.3930, Lon: -70.7858}, ProbeEndpoint: "https://scl.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "BOG", City: "Bogota", Country: "CO", Region: "Latin America", Location: GeoPoint{Lat: 4.7016, Lon: -74.1469}, ProbeEndpoint: "https://bog.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "MEX", City: "Mexico City", Country: "MX", Region: "Latin America", Location: GeoPoint{Lat: 19.4363, Lon: -99.0721}, ProbeEndpoint: "https://mex.cloudflare.com/cdn-cgi/trace", Tier: 1},

	// Middle East & Africa
	{IATA: "DXB", City: "Dubai", Country: "AE", Region: "Middle East & Africa", Location: GeoPoint{Lat: 25.2532, Lon: 55.3657}, ProbeEndpoint: "https://dxb.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "DOH", City: "Doha", Country: "QA", Region: "Middle East & Africa", Location: GeoPoint{Lat: 25.2731, Lon: 51.6081}, ProbeEndpoint: "https://doh.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "RUH", City: "Riyadh", Country: "SA", Region: "Middle East & Africa", Location: GeoPoint{Lat: 24.9576, Lon: 46.6988}, ProbeEndpoint: "https://ruh.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "JNB", City: "Johannesburg", Country: "ZA", Region: "Middle East & Africa", Location: GeoPoint{Lat: -26.1367, Lon: 28.2411}, ProbeEndpoint: "https://jnb.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "CPT", City: "Cape Town", Country: "ZA", Region: "Middle East & Africa", Location: GeoPoint{Lat: -33.9715, Lon: 18.6021}, ProbeEndpoint: "https://cpt.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "NBO", City: "Nairobi", Country: "KE", Region: "Middle East & Africa", Location: GeoPoint{Lat: -1.3192, Lon: 36.9278}, ProbeEndpoint: "https://nbo.cloudflare.com/cdn-cgi/trace", Tier: 2},

	// Oceania
	{IATA: "AKL", City: "Auckland", Country: "NZ", Region: "Oceania", Location: GeoPoint{Lat: -37.0082, Lon: 174.7850}, ProbeEndpoint: "https://akl.cloudflare.com/cdn-cgi/trace", Tier: 1},
	{IATA: "BNE", City: "Brisbane", Country: "AU", Region: "Oceania", Location: GeoPoint{Lat: -27.3842, Lon: 153.1175}, ProbeEndpoint: "https://bne.cloudflare.com/cdn-cgi/trace", Tier: 2},
	{IATA: "PER", City: "Perth", Country: "AU", Region: "Oceania", Location: GeoPoint{Lat: -31.9403, Lon: 115.9668}, ProbeEndpoint: "https://per.cloudflare.com/cdn-cgi/trace", Tier: 2},
}

// DefaultPoPList returns a cloned slice of all registered edge PoPs.
func DefaultPoPList() []PoPInfo {
	list := make([]PoPInfo, len(defaultPoPs))
	copy(list, defaultPoPs)
	return list
}

// LookupPoP searches for an edge PoP by its uppercase IATA code.
func LookupPoP(iata string) (PoPInfo, bool) {
	iataUpper := strings.ToUpper(strings.TrimSpace(iata))
	for _, pop := range defaultPoPs {
		if pop.IATA == iataUpper {
			return pop, true
		}
	}
	return PoPInfo{}, false
}

// ListPoPsByRegion returns all edge PoPs belonging to a specified global region.
func ListPoPsByRegion(region string) []PoPInfo {
	var result []PoPInfo
	for _, pop := range defaultPoPs {
		if strings.EqualFold(pop.Region, strings.TrimSpace(region)) {
			result = append(result, pop)
		}
	}
	return result
}

// CountryToGeo maps a two-letter ISO country code to representative coordinates.
func CountryToGeo(countryCode string) (GeoPoint, bool) {
	switch strings.ToUpper(strings.TrimSpace(countryCode)) {
	case "KR":
		return GeoPoint{Lat: 37.5665, Lon: 126.9780}, true // Seoul
	case "JP":
		return GeoPoint{Lat: 35.6762, Lon: 139.6503}, true // Tokyo
	case "US":
		return GeoPoint{Lat: 38.8951, Lon: -77.0364}, true // Washington DC
	case "GB", "UK":
		return GeoPoint{Lat: 51.5074, Lon: -0.1278}, true // London
	case "DE":
		return GeoPoint{Lat: 50.1109, Lon: 8.6821}, true // Frankfurt
	case "FR":
		return GeoPoint{Lat: 48.8566, Lon: 2.3522}, true // Paris
	case "NL":
		return GeoPoint{Lat: 52.3676, Lon: 4.9041}, true // Amsterdam
	case "SG":
		return GeoPoint{Lat: 1.3521, Lon: 103.8198}, true // Singapore
	case "HK":
		return GeoPoint{Lat: 22.3193, Lon: 114.1694}, true // Hong Kong
	case "TW":
		return GeoPoint{Lat: 25.0330, Lon: 121.5654}, true // Taipei
	case "AU":
		return GeoPoint{Lat: -33.8688, Lon: 151.2093}, true // Sydney
	case "NZ":
		return GeoPoint{Lat: -36.8485, Lon: 174.7633}, true // Auckland
	case "IN":
		return GeoPoint{Lat: 19.0760, Lon: 72.8777}, true // Mumbai
	case "BR":
		return GeoPoint{Lat: -23.5505, Lon: -46.6333}, true // Sao Paulo
	case "AR":
		return GeoPoint{Lat: -34.6037, Lon: -58.3816}, true // Buenos Aires
	case "ZA":
		return GeoPoint{Lat: -26.2041, Lon: 28.0473}, true // Johannesburg
	case "AE":
		return GeoPoint{Lat: 25.2048, Lon: 55.2708}, true // Dubai
	case "CA":
		return GeoPoint{Lat: 43.6532, Lon: -79.3832}, true // Toronto
	default:
		return GeoPoint{}, false
	}
}
