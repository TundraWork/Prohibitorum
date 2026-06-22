-- +goose Up
-- accent_color is a representative sRGB hex ("#rrggbb") extracted from the icon
-- at upload time, used to tint the launchpad tile's backdrop. NULL = not yet
-- computed (legacy rows are healed lazily on first read).
ALTER TABLE entity_icon ADD COLUMN accent_color text;

-- +goose Down
ALTER TABLE entity_icon DROP COLUMN accent_color;
