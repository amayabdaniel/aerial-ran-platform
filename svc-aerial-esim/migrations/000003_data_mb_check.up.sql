SET search_path TO esim, public;

-- data_mb carries -1 as the "unlimited" sentinel (provider.DataUnlimited),
-- deliberately distinct from 0 ("no data"); anything below -1 is corruption.
-- Guard both esim columns that store an allowance so a stray negative cannot
-- land. The floor is -1, NOT 0: a CHECK (data_mb >= 0) would reject every
-- unlimited plan, conflating "unlimited" with invalid. NULL stays permitted on
-- esims.data_mb — a CHECK passes on NULL (it is UNKNOWN, not FALSE).
ALTER TABLE esims
    ADD CONSTRAINT esims_data_mb_check CHECK (data_mb >= -1);
ALTER TABLE packages
    ADD CONSTRAINT packages_data_mb_check CHECK (data_mb >= -1);
