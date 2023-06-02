package db

var (
	sqlInsertProvider = `
		insert into providers(pubkey,service,bond) values ($1,$2,$3) returning id, created, updated
	`

	sqlUpdateProvider = `
		update providers
		set bond = $3,
		    metadata_uri = $4,
			metadata_nonce = $5,
			status = $6,
			min_contract_duration = $7,
			max_contract_duration = $8,
			settlement_duration = $9,
			updated = now()
		where pubkey = $1
		  and service = $2
		returning id, created, updated
	`

	sqlFindProvider = `
		select 
			id,
			created,
			updated,
			pubkey,
			service,
			coalesce(bond,0) as bond,
			coalesce(metadata_uri,'') as metadata_uri,
			coalesce(metadata_nonce,0) as metadata_nonce,
			coalesce(status,'OFFLINE') as status,
			coalesce(min_contract_duration,-1) as min_contract_duration,
			coalesce(max_contract_duration,-1) as max_contract_duration,
			coalesce(settlement_duration,-1) as settlement_duration
		from providers p
		where p.pubkey = $1
		  and p.service = $2
	`
	sqlInsertBondProviderEvent = `insert into provider_bond_events(provider_id,height,txid,bond_rel,bond_abs) values ($1,$2,$3,$4,$5)
		on conflict on constraint provider_bond_events_txid_unq
		do update set updated = now()
		where provider_bond_events.txid = $3
		returning id, created, updated
	`
	sqlInsertModProviderEvent = `insert into provider_mod_events(provider_id,height,txid,metadata_uri,metadata_nonce,status,min_contract_duration,max_contract_duration)
		values ($1,$2,$3,$4,$5,$6,$7,$8)
		on conflict on constraint provider_mod_events_txid_unq
		do update set updated = now()
		where provider_mod_events.txid = $3
		returning id, created, updated
	`
	sqlUpsertProviderMetadata = `insert into provider_metadata(provider_id,nonce,moniker,website,description,location,free_rate_limit)
		values ($1,$2,$3,$4,$5,CAST(NULLIF($6, '') AS point),$7,$8)
		on conflict on constraint prov_metanonce_uniq
		do update set updated = now()
		where provider_metadata.provider_id = $1
		  and provider_metadata.nonce = $2
		returning id, created, updated
	`
	sqlUpsertValidatorPayoutEvent = `insert into validator_payout_events(validator,height,paid)
	values ($1,$2,$3)
	on conflict on constraint validator_payout_evts_validator_height_key
	do update set updated = now()
	where validator_payout_events.validator = $1
	  and validator_payout_events.height = $2
	returning id, created, updated
	`

	sqlDeleteSubscriptionRates = `
		DELETE FROM provider_subscription_rates
		WHERE provider_id IN (
			SELECT id
			FROM providers
			WHERE pubkey = $1 AND service = $2
		);
	`

	sqlInsertSubscriptionRates = `
		INSERT INTO provider_subscription_rates (provider_id, token_name, token_amount) VALUES
	`

	sqlFindProviderSubscriptionRates = `
		SELECT * FROM provider_subscription_rates
        WHERE provider_id = $1
	`

	sqlDeletePayAsYouGoRates = `
		DELETE FROM provider_pay_as_you_go_rates
		WHERE provider_id IN (
			SELECT id
			FROM providers
			WHERE pubkey = $1 AND service = $2
		);
	`

	sqlInsertPayAsYouGoRates = `
		INSERT INTO provider_pay_as_you_go_rates (provider_id, token_name, token_amount) VALUES
	`

	sqlFindProviderPayAsYouGoRates = `
		SELECT * FROM provider_pay_as_you_go_rates
        WHERE provider_id = $1
	`
)
