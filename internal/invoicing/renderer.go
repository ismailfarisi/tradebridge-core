package invoicing

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Party is a name-only participant shown on the rendered invoice (seller
// "From" or buyer "Bill to").
type Party struct {
	Name string
}

// Render draws a simple A4 invoice PDF: header, seller/buyer blocks, a
// line-items table, and subtotal/total. It is pure — no I/O, no DB — so it
// is trivially unit-testable and reusable by both the download handler and
// (later) email attachments.
func Render(inv sqlc.Invoice, lines []sqlc.InvoiceLineItem, seller, buyer Party) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Invoice "+inv.InvoiceNumber, false)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 20)
	pdf.Cell(0, 12, "INVOICE")
	pdf.Ln(14)

	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 6, inv.InvoiceNumber)
	pdf.Ln(6)
	pdf.Cell(0, 6, "Issued: "+inv.IssuedAt.Format("2006-01-02"))
	pdf.Ln(12)

	// From / Bill to
	pdf.SetFont("Helvetica", "B", 11)
	pdf.Cell(95, 6, "From")
	pdf.Cell(95, 6, "Bill to")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(95, 6, seller.Name)
	pdf.Cell(95, 6, buyer.Name)
	pdf.Ln(12)

	// Table header
	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(100, 8, "Description")
	pdf.Cell(20, 8, "Qty")
	pdf.Cell(35, 8, "Unit")
	pdf.Cell(35, 8, "Amount")
	pdf.Ln(8)

	pdf.SetFont("Helvetica", "", 10)
	money := func(c int64) string { return fmt.Sprintf("%s %.2f", inv.Currency, float64(c)/100) }
	for _, li := range lines {
		pdf.Cell(100, 8, li.Description)
		pdf.Cell(20, 8, fmt.Sprintf("%d", li.Quantity))
		pdf.Cell(35, 8, money(li.UnitPriceCents))
		pdf.Cell(35, 8, money(li.AmountCents))
		pdf.Ln(8)
	}

	pdf.Ln(4)
	pdf.Cell(120, 8, "")
	pdf.Cell(35, 8, "Subtotal")
	pdf.Cell(35, 8, money(inv.SubtotalCents))
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.Cell(120, 8, "")
	pdf.Cell(35, 8, "Total")
	pdf.Cell(35, 8, money(inv.TotalCents))
	pdf.Ln(8)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	if err := pdf.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
