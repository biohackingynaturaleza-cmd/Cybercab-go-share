-- Referencia del viaje en la flota.
--
-- En el modelo de traspaso, quien organiza pide el coche en la app de Tesla y
-- registra aquí su referencia. Es lo único que ata nuestro trayecto con el
-- viaje real, así que sin ella no se puede reclamar nada ni auditar nada.

ALTER TABLE trips ADD COLUMN IF NOT EXISTS fleet_ride_ref TEXT NOT NULL DEFAULT '';

-- Una misma referencia no puede estar en dos trayectos: sería el mismo viaje
-- cobrado dos veces.
CREATE UNIQUE INDEX IF NOT EXISTS trips_fleet_ride_ref
    ON trips (fleet_ride_ref)
    WHERE fleet_ride_ref <> '';
