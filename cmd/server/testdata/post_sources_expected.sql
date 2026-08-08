UPDATE ConfigTable SET UTCModDate=[TIMESTAMP_UPDATED] WHERE RecID=1;
UPDATE SourceTable SET Name='Public Domain GEDCOM file imported on 22 July 2026', IsPrivate=1, UTCModDate=[TIMESTAMP_UPDATED] WHERE SourceID=1;
