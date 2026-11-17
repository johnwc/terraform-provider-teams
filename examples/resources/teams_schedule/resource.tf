resource "teams_schedule" "business_hours" {
  name = "Business hours"

  weekly {
    monday    = [{ start = "08:00", end = "12:00" }, { start = "13:00", end = "17:00" }]
    tuesday   = [{ start = "08:00", end = "17:00" }]
    wednesday = [{ start = "08:00", end = "17:00" }]
    thursday  = [{ start = "08:00", end = "17:00" }]
    friday    = [{ start = "08:00", end = "16:00" }]
  }
}

resource "teams_schedule" "holidays" {
  name = "Holidays 2026"

  fixed {
    date_range = [
      { start = "2026-12-25T00:00:00", end = "2026-12-26T00:00:00" },
      { start = "2026-12-31T12:00:00", end = "2027-01-02T00:00:00" },
    ]
  }
}
