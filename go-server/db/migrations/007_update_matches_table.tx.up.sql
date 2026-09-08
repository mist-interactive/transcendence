/*
  Alter table matches to include player scores
*/

ALTER TABLE matches
ADD COLUMN player_one_score INT NULL,
ADD COLUMN player_two_score INT NULL;
