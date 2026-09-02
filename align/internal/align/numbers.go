package align

import "strconv"

// Numerals are the single largest systematic disagreement between an EPUB
// and its narration: the book prints "23" and Whisper writes "twenty-three"
// (or the reverse, depending on the model's mood). Neither side is wrong,
// so both are folded to one form before matching — digits become words,
// because that direction needs no multi-word parsing and is a pure
// token-for-tokens expansion.
//
// Expansion is the plain reading with no "and": 1904 is "one thousand nine
// hundred four". A narrator saying "nineteen oh four" therefore still
// disagrees, and that is accepted: it is four tokens of local noise inside
// a window of a hundred and fifty, which the banded alignment absorbs.

var onesWords = [...]string{
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight",
	"nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
	"sixteen", "seventeen", "eighteen", "nineteen",
}

var tensWords = [...]string{
	"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy",
	"eighty", "ninety",
}

// ordinalWords is the last-word substitution that turns a cardinal reading
// into an ordinal one: "twenty three" -> "twenty third".
var ordinalWords = map[string]string{
	"zero": "zeroth", "one": "first", "two": "second", "three": "third",
	"four": "fourth", "five": "fifth", "six": "sixth", "seven": "seventh",
	"eight": "eighth", "nine": "ninth", "ten": "tenth", "eleven": "eleventh",
	"twelve": "twelfth", "thirteen": "thirteenth", "fourteen": "fourteenth",
	"fifteen": "fifteenth", "sixteen": "sixteenth", "seventeen": "seventeenth",
	"eighteen": "eighteenth", "nineteen": "nineteenth", "twenty": "twentieth",
	"thirty": "thirtieth", "forty": "fortieth", "fifty": "fiftieth",
	"sixty": "sixtieth", "seventy": "seventieth", "eighty": "eightieth",
	"ninety": "ninetieth", "hundred": "hundredth", "thousand": "thousandth",
	"million": "millionth",
}

// maxNumeralDigits bounds what is treated as a number at all. Past nine
// digits a run of digits is an identifier, a phone number or an ISBN — not
// something a narrator reads as a quantity — and expanding it would produce
// a dozen junk tokens.
const maxNumeralDigits = 9

// appendCardinal writes the plain English reading of n onto dst.
func appendCardinal(dst []string, n uint64) []string {
	switch {
	case n >= 1_000_000:
		dst = appendCardinal(dst, n/1_000_000)
		dst = append(dst, "million")
		if r := n % 1_000_000; r > 0 {
			dst = appendCardinal(dst, r)
		}
	case n >= 1_000:
		dst = appendCardinal(dst, n/1_000)
		dst = append(dst, "thousand")
		if r := n % 1_000; r > 0 {
			dst = appendCardinal(dst, r)
		}
	case n >= 100:
		dst = append(dst, onesWords[n/100], "hundred")
		if r := n % 100; r > 0 {
			dst = appendCardinal(dst, r)
		}
	case n >= 20:
		dst = append(dst, tensWords[n/10])
		if r := n % 10; r > 0 {
			dst = append(dst, onesWords[r])
		}
	default:
		dst = append(dst, onesWords[n])
	}
	return dst
}

// appendOrdinal writes the ordinal reading of n: 21 -> "twenty first".
func appendOrdinal(dst []string, n uint64) []string {
	start := len(dst)
	dst = appendCardinal(dst, n)
	last := len(dst) - 1
	if ord, ok := ordinalWords[dst[last]]; ok {
		dst[last] = ord
	} else if last > start {
		// Unreachable with the table above, but a cardinal whose last
		// word has no ordinal form is better left alone than corrupted.
		dst[last] = dst[last] + "th"
	}
	return dst
}

// appendNumeral expands a token that is a written number and reports
// whether it did. The token has already been through booktext.Normalize,
// so it is letters and digits only: "23", "1904", "21st", "12th".
func appendNumeral(dst []string, word string) ([]string, bool) {
	digits := word
	ordinal := false
	if len(word) > 2 {
		switch word[len(word)-2:] {
		case "st", "nd", "rd", "th":
			digits, ordinal = word[:len(word)-2], true
		}
	}
	if digits == "" || len(digits) > maxNumeralDigits {
		return dst, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return dst, false
		}
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return dst, false
	}
	if ordinal {
		return appendOrdinal(dst, n), true
	}
	return appendCardinal(dst, n), true
}
