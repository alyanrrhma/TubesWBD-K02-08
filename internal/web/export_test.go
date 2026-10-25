package web

// FormatCellForTest membuka formatCell untuk test eksternal (package web_test)
func FormatCellForTest(v any) CellView { return formatCell(v) }