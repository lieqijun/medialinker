package main

import "strings"

var cnDigits = map[rune]int{
	'零': 0, '〇': 0,
	'一': 1, '壹': 1,
	'二': 2, '两': 2, '贰': 2,
	'三': 3, '叁': 3,
	'四': 4, '肆': 4,
	'五': 5, '伍': 5,
	'六': 6, '陆': 6,
	'七': 7, '柒': 7,
	'八': 8, '捌': 8,
	'九': 9, '玖': 9,
}

var cnUnits = map[rune]int{
	'十': 10, '拾': 10,
	'百': 100, '佰': 100,
	'千': 1000, '仟': 1000,
}

// chineseToNumber 把中文数字（十二 / 二十三 / 一百 / 两）转成整数。
// 支持阿拉伯数字混写，超过 9999 的场景不处理（季集数不会那么大）。
// 无法识别时返回 0。
func chineseToNumber(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// 纯阿拉伯数字直接返回
	if n, ok := atoiStrict(s); ok {
		return n
	}

	section := 0
	number := 0
	seenUnit := false

	for _, r := range s {
		if d, ok := cnDigits[r]; ok {
			number = d
			continue
		}
		if u, ok := cnUnits[r]; ok {
			if number == 0 {
				// “十二” 这种省略了前面的“一”
				number = 1
			}
			section += number * u
			number = 0
			seenUnit = true
			continue
		}
		if r >= '0' && r <= '9' {
			number = number*10 + int(r-'0')
			continue
		}
		// 其它字符（空格、符号）忽略
	}

	total := section + number
	if !seenUnit && total == 0 && number == 0 {
		return 0
	}
	return total
}

// atoiStrict 只接受纯数字字符串
func atoiStrict(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > 1000000 {
			return 0, false
		}
	}
	return n, true
}
