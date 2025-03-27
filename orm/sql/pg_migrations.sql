-- version 1
-- 添加no_data字段到khole表
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'khole' AND column_name = 'no_data') THEN
        ALTER TABLE public.khole ADD COLUMN no_data BOOLEAN DEFAULT FALSE;
    END IF;
END $$;

-- version 2
-- exsymbol表的symbol字段改为最大长度50
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
        WHERE table_name = 'exsymbol' AND column_name = 'symbol' AND character_maximum_length = 50
    ) THEN
    ALTER TABLE public.exsymbol ALTER COLUMN symbol TYPE varchar(50);
    END IF;
END $$;
