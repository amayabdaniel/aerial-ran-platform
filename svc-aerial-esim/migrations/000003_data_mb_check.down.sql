SET search_path TO esim, public;

ALTER TABLE esims    DROP CONSTRAINT IF EXISTS esims_data_mb_check;
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_data_mb_check;
