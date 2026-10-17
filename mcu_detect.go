package main

import (
	"regexp"
	"sort"
	"strings"
)

const maxMCUCandidates = 16

type mcuCandidate struct {
	Model, Family, Vendor, Core, Evidence string
	Strength                              int
}

type mcuRule struct {
	pattern              *regexp.Regexp
	family, vendor, core string
}

// Match explicit chip names, not SDK/RTOS names, UART bridges or generic ACKs.
var mcuRules = []mcuRule{
	{regexp.MustCompile(`^ESP32(?:-(?:S[23]|C[2356]|C61|H2|P4))?$`), "ESP32", "Espressif / 乐鑫", "依具体型号而定"},
	{regexp.MustCompile(`^ESP8266(?:EX)?$`), "ESP8266", "Espressif / 乐鑫", "Tensilica L106"},
	{regexp.MustCompile(`^STM32(?:[CFGHULW][0-9][0-9A-Z]{0,12})?$`), "STM32", "STMicroelectronics / 意法半导体", "依具体系列而定"},
	{regexp.MustCompile(`^GD32(?:(?:VF|[AEFHLVW])[0-9][0-9A-Z]{0,12})?$`), "GD32", "GigaDevice / 兆易创新", "依具体系列而定"},
	{regexp.MustCompile(`^CH32(?:[FVX][0-9][0-9A-Z]{0,10})?$`), "CH32", "WCH / 沁恒", "依具体系列而定"},
	{regexp.MustCompile(`^NRF(?:(?:51|52)(?:[0-9]{3})?|53(?:[0-9]{2})?|54(?:[HL][0-9]{2})?)(?:_[A-Z0-9]{2,8})?$`), "nRF", "Nordic Semiconductor", "依具体系列而定"},
	{regexp.MustCompile(`^RP(?:2040|2350[AB]?)$`), "RP", "Raspberry Pi", "依具体型号而定"},
	{regexp.MustCompile(`^ATMEGA[0-9]{1,4}[A-Z]{0,3}$`), "ATmega", "Microchip / Atmel", "AVR 8-bit"},
	{regexp.MustCompile(`^ATTINY[0-9]{1,4}[A-Z]{0,3}$`), "ATtiny", "Microchip / Atmel", "AVR 8-bit"},
	{regexp.MustCompile(`^LPC[0-9]{3,4}[A-Z0-9]{0,8}$`), "LPC", "NXP", "依具体系列而定"},
	{regexp.MustCompile(`^ATSAM[0-9A-Z]{3,14}$`), "SAM", "Microchip / Atmel", "依具体系列而定"},
	{regexp.MustCompile(`^PIC(?:1[068]|24|32)[A-Z0-9]{2,14}$`), "PIC", "Microchip", "依具体系列而定"},
}

var espROMModel = regexp.MustCompile(`^ESP32(S[23]|C[2356]|C61|H2|P4)(?:-(?:API|RC)[0-9]+)?(?:-[0-9]{8})?$`)

// All state is UI-thread owned and bounded independently of the display log.
// Tokens and ANSI sequences can span arbitrary serial Read boundaries.
type mcuDetector struct {
	ansi       ansiStream
	token      string
	overlong   bool
	bootTail   string
	candidates []mcuCandidate
	bytes      uint64
	limited    bool
}

func (d *mcuDetector) feed(data []byte) {
	d.bytes += uint64(len(data))
	d.scan(data)
}

func (d *mcuDetector) scan(data []byte) {
	// Preserve binary/control boundaries that the display's plain text omits.
	clean := append([]byte(nil), data...)
	for i, ch := range clean {
		if ch < 0x20 && ch != 0x1b && ch != '\a' {
			clean[i] = ' '
		}
	}
	_, plain := d.ansi.feed(clean)
	plain = strings.ToUpper(plain)
	for _, ch := range []byte(plain) {
		if ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			if len(d.token) < 64 && !d.overlong {
				d.token += string(ch)
			} else {
				d.token, d.overlong = "", true
			}
		} else {
			if !d.overlong {
				if candidate, ok := matchMCUToken(d.token); ok {
					d.remember(candidate)
				}
			}
			d.token, d.overlong = "", false
		}
	}
	// Dated Espressif ROM banners are weak evidence without a model name.
	boot := d.bootTail + plain
	if strings.Contains(boot, "ETS JAN  8 2013") || strings.Contains(boot, "ETS JAN 8 2013") {
		d.remember(mcuCandidate{Model: "ESP8266", Family: "ESP8266", Vendor: "Espressif / 乐鑫", Core: "Tensilica L106", Strength: 1, Evidence: "RX 启动特征：ets Jan 8 2013"})
	}
	for _, signature := range []string{"ETS JUN  8 2016", "ETS JUN 8 2016", "ETS JUL 29 2019"} {
		if strings.Contains(boot, signature) {
			d.remember(mcuCandidate{Model: "ESP32", Family: "ESP32", Vendor: "Espressif / 乐鑫", Core: "依具体 ESP32 型号而定", Strength: 1, Evidence: "RX 启动特征：" + signature})
		}
	}
	d.bootTail = strings.Clone(boot[max(0, len(boot)-32):])
}

func matchMCUToken(token string) (mcuCandidate, bool) {
	if len(token) < 4 {
		return mcuCandidate{}, false
	}
	// Newer ESP ROM banners append their build date to the model token.
	if match := espROMModel.FindStringSubmatch(token); match != nil {
		token = "ESP32-" + match[1]
	}
	if strings.HasPrefix(token, "ESP32-") {
		if i := strings.LastIndexByte(token, '-'); i > 5 && len(token)-i-1 == 8 {
			date := token[i+1:]
			if strings.IndexFunc(date, func(ch rune) bool { return ch < '0' || ch > '9' }) == -1 {
				token = token[:i]
			}
		}
	}
	for _, rule := range mcuRules {
		if !rule.pattern.MatchString(token) {
			continue
		}
		strength := 3
		if token == rule.family || token == "NRF51" || token == "NRF52" || token == "NRF53" || token == "NRF54" ||
			(rule.family == "STM32" || rule.family == "GD32" || rule.family == "CH32") && len(token) <= len(rule.family)+2 {
			strength = 2
		}
		core := rule.core
		switch {
		case token == "ESP32":
			core = "依具体 ESP32 型号而定"
		case token == "ESP32-S2" || token == "ESP32-S3":
			core = "Xtensa LX7"
		case strings.HasPrefix(token, "ESP32-"):
			core = "RISC-V"
		case strings.HasPrefix(token, "STM32F1"), strings.HasPrefix(token, "STM32F2"):
			core = "Arm Cortex-M3"
		case strings.HasPrefix(token, "STM32F4"), strings.HasPrefix(token, "STM32F3"):
			core = "Arm Cortex-M4"
		case strings.HasPrefix(token, "CH32V"), strings.HasPrefix(token, "CH32X"), strings.HasPrefix(token, "GD32VF"):
			core = "RISC-V"
		case strings.HasPrefix(token, "NRF52"):
			core = "Arm Cortex-M4"
		case token == "RP2040":
			core = "双核 Arm Cortex-M0+"
		case strings.HasPrefix(token, "RP2350"):
			core = "Arm Cortex-M33 / Hazard3 RISC-V"
		case strings.HasPrefix(token, "ATSAMD21"):
			core = "Arm Cortex-M0+"
		case strings.HasPrefix(token, "PIC18"):
			core = "PIC18 8-bit"
		}
		return mcuCandidate{Model: token, Family: rule.family, Vendor: rule.vendor, Core: core, Strength: strength, Evidence: "RX 型号标记：" + token}, true
	}
	return mcuCandidate{}, false
}

func (d *mcuDetector) remember(candidate mcuCandidate) {
	for i, old := range d.candidates {
		if old.Model == candidate.Model {
			if candidate.Strength > old.Strength {
				d.candidates[i] = candidate
			}
			return
		}
	}
	if len(d.candidates) < maxMCUCandidates {
		d.candidates = append(d.candidates, candidate)
	} else {
		d.limited = true
	}
}

func (d *mcuDetector) results() []mcuCandidate {
	// Preview an unfinished token without committing a partial model to history.
	copy := *d
	copy.candidates = append([]mcuCandidate(nil), d.candidates...)
	if candidate, ok := matchMCUToken(d.token); ok && !d.overlong {
		copy.remember(candidate)
	}
	var results []mcuCandidate
	for _, candidate := range copy.candidates {
		superseded := false
		for _, other := range copy.candidates {
			// Refine only this family/series, never hide an unrelated series.
			if other.Family == candidate.Family && other.Model != candidate.Model &&
				(candidate.Model == strings.ToUpper(candidate.Family) || candidate.Strength == 1 && other.Strength > 1 ||
					(candidate.Family == "STM32" || candidate.Family == "GD32" || candidate.Family == "CH32" || candidate.Family == "nRF") &&
						strings.HasPrefix(other.Model, candidate.Model) && len(other.Model) > len(candidate.Model)) {
				superseded = true
				break
			}
		}
		if !superseded {
			results = append(results, candidate)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Strength != results[j].Strength {
			return results[i].Strength > results[j].Strength
		}
		return results[i].Model < results[j].Model
	})
	return results
}
